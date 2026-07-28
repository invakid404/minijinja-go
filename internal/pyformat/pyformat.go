// Package pyformat implements Python's two string-formatting styles.
//
// It is the Go port of the engine's format_utils.rs (boundaryml/minijinja
// @8cfc770), which backs two things the fork must match byte for byte:
//
//   - the `format` filter, which is printf style: `{{ "%s!"|format(name) }}`
//     (filters.rs:1689-1691);
//   - pycompat's `str.format`, which is the `{}` style:
//     `{{ "{}!".format(name) }}` (pycompat.rs:194).
//
// Both styles share one format-spec model — fill/align, sign, alternate form,
// zero padding, width, digit grouping, precision and conversion type — and
// differ only in how a replacement field is spelled and in a handful of
// deliberate quirks that Python itself has (printf right-aligns strings and
// str.format left-aligns them; `%s` accepts a bool and `{:s}` does not).
//
// Widths and precisions count BYTES, not characters, because the reference
// implementation slices and measures Rust strings. That is a real observable:
// `"{:.1}".format("日")` cuts a multi-byte character in half.
package pyformat

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	mjerrors "github.com/invakid404/minijinja-go/v2/internal/errors"
	"github.com/invakid404/minijinja-go/v2/value"
)

// Style selects the format-string dialect.
type Style int

const (
	// StylePrintf is printf-style: "%s, %s!"
	StylePrintf Style = iota
	// StyleStrFormat is str.format style: "{}, {name}!"
	StyleStrFormat
)

func invalidOp(format string, args ...any) error {
	return mjerrors.NewError(mjerrors.ErrInvalidOperation, fmt.Sprintf(format, args...))
}

// Format applies args to a format string. kwargs is only consulted by
// StyleStrFormat, where a replacement field may name one.
func Format(style Style, formatStr string, args []value.Value, kwargs map[string]value.Value) (string, error) {
	p := &parser{src: formatStr, style: style}
	var out strings.Builder
	argIndex := 0
	autoNumbering := false
	manualNumbering := false

	for {
		tok, err := p.next()
		if err != nil {
			return "", err
		}
		if tok == nil {
			break
		}
		if tok.literal != "" || tok.isLiteral {
			out.WriteString(tok.literal)
			continue
		}

		var arg value.Value
		switch {
		case tok.field.mappingKey != nil:
			// printf `%(key)s`: the sole argument must be a mapping.
			if len(args) == 0 {
				return "", missingPrintfArg(tok.spec.location)
			}
			if args[0].Kind() != value.KindMap {
				return "", invalidOp("format argument must be a mapping")
			}
			got := args[0].GetAttr(*tok.field.mappingKey)
			if got.IsUndefined() {
				return "", missingPrintfArg(tok.spec.location)
			}
			arg = got

		case tok.field.kwarg != nil:
			val, ok := kwargs[*tok.field.kwarg]
			if !ok {
				return "", invalidOp("argument not found for format field at offset %d", tok.location)
			}
			nested, err := nestedValue(val, tok.field.path)
			if err != nil {
				return "", invalidOp("argument not found for format field at offset %d", tok.location)
			}
			arg = nested

		case tok.field.positional != nil:
			manualNumbering = true
			if autoNumbering {
				return "", invalidOp(
					"cannot switch from automatic numbering to manual field specification in field at offset %d",
					tok.location)
			}
			idx := *tok.field.positional
			if idx >= len(args) {
				return "", invalidOp("argument not found for format field at offset %d", tok.location)
			}
			nested, err := nestedValue(args[idx], tok.field.path)
			if err != nil {
				return "", invalidOp("argument not found for format field at offset %d", tok.location)
			}
			arg = nested

		default:
			if p.style == StylePrintf {
				if argIndex >= len(args) {
					return "", missingPrintfArg(tok.spec.location)
				}
				arg = args[argIndex]
				argIndex++
				break
			}
			autoNumbering = true
			if manualNumbering {
				return "", invalidOp(
					"cannot switch from manual field specification to automatic numbering in field at offset %d",
					tok.location)
			}
			if argIndex >= len(args) {
				return "", invalidOp("argument not found for format field at offset %d", tok.location)
			}
			arg = args[argIndex]
			argIndex++
		}

		formatted, err := tok.spec.format(arg)
		if err != nil {
			return "", err
		}
		out.WriteString(formatted)
	}
	return out.String(), nil
}

func missingPrintfArg(location int) error {
	return invalidOp("missing an argument for format spec at offset '%d'", location)
}

// nestedValue walks a str.format field path: `{a.b[0]}`.
func nestedValue(root value.Value, path []pathElem) (value.Value, error) {
	curr := root
	for _, elem := range path {
		if elem.attr {
			curr = curr.GetAttr(elem.name)
			continue
		}
		if idx, err := strconv.Atoi(elem.name); err == nil && idx >= 0 {
			curr = curr.GetItem(value.FromInt(int64(idx)))
			continue
		}
		curr = curr.GetAttr(elem.name)
	}
	if curr.IsUndefined() {
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrUndefinedVar, "undefined value")
	}
	return curr, nil
}

// --- the format spec -------------------------------------------------------

type align int

const (
	alignLeft align = iota
	alignRight
	alignCenter
)

type convType int

const (
	typeDefault convType = iota
	typeBinary
	typeDecimal
	typeOctal
	typeLowerHex
	typeUpperHex
	typeLowerE
	typeUpperE
	typeLowerF
	typeUpperF
	typeLowerG
	typeUpperG
	typeString
)

func (t convType) description() string {
	switch t {
	case typeBinary:
		return "binary format ('b')"
	case typeOctal:
		return "octal format ('o')"
	case typeLowerHex:
		return "hex format ('x')"
	case typeUpperHex:
		return "hex format ('X')"
	case typeDecimal:
		return "decimal format ('d')"
	case typeLowerE:
		return "scientific notation ('e')"
	case typeUpperE:
		return "scientific notation ('E')"
	case typeLowerF:
		return "fixed-point notation ('f')"
	case typeUpperF:
		return "fixed-point notation ('F')"
	case typeLowerG:
		return "general format ('g')"
	case typeUpperG:
		return "general format ('G')"
	case typeString:
		return "string format ('s')"
	default:
		return ""
	}
}

type formatSpec struct {
	hasFillAlign bool
	fill         rune
	hasFill      bool
	fillAlign    align

	printSign           bool
	spaceBeforePositive bool
	alternateForm       bool
	zeroPadded          bool
	width               int
	hasWidth            bool
	grouping            byte // 0, ',' or '_'
	precision           int
	hasPrecision        bool
	ty                  convType
	style               Style
	location            int
}

func (s *formatSpec) typeConversionErr(kind string, ty convType) error {
	return invalidOp("invalid format spec at offset %d; '%s' cannot be formatted in %s",
		s.location, kind, ty.description())
}

func (s *formatSpec) format(val value.Value) (string, error) {
	if b, ok := val.AsBool(); ok && val.Kind() == value.KindBool {
		return s.formatBool(b)
	}
	if val.Kind() == value.KindNumber && val.IsActualInt() {
		n, _ := val.AsInt()
		magnitude := uint64(n)
		if n < 0 {
			magnitude = uint64(-(n + 1)) + 1
		}
		return s.formatInteger(magnitude, n < 0)
	}
	if f, ok := val.AsFloat(); ok && val.Kind() == value.KindNumber {
		return s.formatFloat(f)
	}
	return s.formatString(val.String())
}

func (s *formatSpec) formatBool(val bool) (string, error) {
	treatAsInteger := s.hasFillAlign || s.printSign || s.alternateForm ||
		s.zeroPadded || s.hasWidth || s.hasPrecision

	switch {
	case s.ty == typeDefault && !treatAsInteger:
		// "true"/"false" as a plain string, precision ignored.
		return s.applyPadding(strconv.FormatBool(val), alignLeft), nil
	case s.ty == typeString:
		if s.style == StylePrintf {
			return s.applyPadding(strconv.FormatBool(val), alignRight), nil
		}
		return "", s.typeConversionErr("bool", typeString)
	default:
		n := uint64(0)
		if val {
			n = 1
		}
		return s.formatInteger(n, false)
	}
}

func (s *formatSpec) formatString(text string) (string, error) {
	switch s.ty {
	case typeDefault, typeString:
		defaultAlign := alignLeft
		if s.style == StylePrintf {
			defaultAlign = alignRight
		}
		// Byte slicing, as in the reference implementation.
		if s.hasPrecision && s.precision < len(text) {
			return s.applyPadding(text[:s.precision], defaultAlign), nil
		}
		return s.applyPadding(text, defaultAlign), nil
	default:
		return "", s.typeConversionErr("string", s.ty)
	}
}

// mantissaAndExp is Rust's `format!("{val:.precision$e}")`, split into its
// mantissa and its integer exponent.
func mantissaAndExp(val float64, precision int) (string, int) {
	formatted := strconv.FormatFloat(val, 'e', precision, 64)
	idx := strings.LastIndexByte(formatted, 'e')
	exp, _ := strconv.Atoi(formatted[idx+1:])
	return formatted[:idx], exp
}

func (s *formatSpec) fixDecimalPoint(num string) string {
	if s.hasPrecision && s.precision == 0 && s.alternateForm {
		return num + "."
	}
	return num
}

func (s *formatSpec) removeInsignificants(num string) string {
	if !s.alternateForm && strings.Contains(num, ".") {
		return strings.TrimRight(strings.TrimRight(num, "0"), ".")
	}
	return num
}

func (s *formatSpec) numberInGeneralFormat(val float64, isUpper bool) string {
	precision := 6
	if s.hasPrecision {
		precision = s.precision
		if precision == 0 {
			precision = 1
		}
	}

	manti, exp := mantissaAndExp(val, precision-1)
	if exp >= -4 && exp < precision {
		decimalPlaces := precision - 1 - exp
		num := strconv.FormatFloat(val, 'f', decimalPlaces, 64)
		return s.groupDecimalNum(s.removeInsignificants(num))
	}
	manti = s.groupDecimalNum(s.removeInsignificants(manti))
	e := "e"
	if isUpper {
		e = "E"
	}
	return fmt.Sprintf("%s%s%s", manti, e, signedExp(exp))
}

// signedExp is Rust's `{exp:+03}`: a sign and at least two digits.
func signedExp(exp int) string {
	sign := "+"
	if exp < 0 {
		sign = "-"
		exp = -exp
	}
	digits := strconv.Itoa(exp)
	if len(digits) < 2 {
		digits = "0" + digits
	}
	return sign + digits
}

// group splits num into chunks of groupSize from the right.
func group(num string, separator byte, groupSize int) string {
	prefixLen := len(num) % groupSize
	var b strings.Builder
	b.WriteString(num[:prefixLen])
	for i := prefixLen; i < len(num); i += groupSize {
		if b.Len() > 0 {
			b.WriteByte(separator)
		}
		b.WriteString(num[i : i+groupSize])
	}
	return b.String()
}

func (s *formatSpec) groupBinaryNum(number string) (string, error) {
	switch s.grouping {
	case ',':
		return "", invalidOp("invalid format spec at offset %d; ',' cannot be specified with %s",
			s.location, s.ty.description())
	case '_':
		return group(number, '_', 4), nil
	default:
		return number, nil
	}
}

func (s *formatSpec) groupDecimalNum(number string) string {
	if s.grouping == 0 {
		return number
	}
	separator := s.grouping
	integer, fraction, hasPoint := strings.Cut(number, ".")
	integer = group(integer, separator, 3)
	if hasPoint {
		return integer + "." + fraction
	}
	return integer
}

func (s *formatSpec) formatInteger(val uint64, isNegative bool) (string, error) {
	sign := ""
	switch {
	case isNegative:
		sign = "-"
	case s.printSign:
		sign = "+"
	case s.spaceBeforePositive:
		sign = " "
	}

	var number string
	switch s.ty {
	case typeBinary:
		n, err := s.groupBinaryNum(strconv.FormatUint(val, 2))
		if err != nil {
			return "", err
		}
		number = n
	case typeOctal:
		n, err := s.groupBinaryNum(strconv.FormatUint(val, 8))
		if err != nil {
			return "", err
		}
		number = n
	case typeLowerHex:
		n, err := s.groupBinaryNum(strconv.FormatUint(val, 16))
		if err != nil {
			return "", err
		}
		number = n
	case typeUpperHex:
		n, err := s.groupBinaryNum(strings.ToUpper(strconv.FormatUint(val, 16)))
		if err != nil {
			return "", err
		}
		number = n
	case typeDefault, typeDecimal:
		number = s.groupDecimalNum(strconv.FormatUint(val, 10))
	case typeString:
		if s.style != StylePrintf {
			return "", s.typeConversionErr("integer", typeString)
		}
		// printf-style Python ignores '+' when combined with 's'.
		sign = ""
		if isNegative {
			sign = "-"
		}
		number = strconv.FormatUint(val, 10)
	case typeLowerE, typeUpperE:
		precision := 6
		if s.hasPrecision {
			precision = s.precision
		}
		mant, exp := mantissaAndExp(float64(val), precision)
		mant = s.groupDecimalNum(s.fixDecimalPoint(mant))
		e := "e"
		if s.ty == typeUpperE {
			e = "E"
		}
		number = fmt.Sprintf("%s%s%s", mant, e, signedExp(exp))
	case typeLowerF, typeUpperF:
		precision := 6
		if s.hasPrecision {
			precision = s.precision
		}
		num := strconv.FormatUint(val, 10)
		if precision != 0 {
			num += "." + strings.Repeat("0", precision)
		}
		number = s.groupDecimalNum(s.fixDecimalPoint(num))
	case typeLowerG, typeUpperG:
		number = s.numberInGeneralFormat(float64(val), s.ty == typeUpperG)
	}

	return s.formatNumber(number, sign), nil
}

func (s *formatSpec) formatFloat(val float64) (string, error) {
	sign := ""
	switch {
	case math.Signbit(val):
		sign = "-"
	case s.printSign && s.ty != typeString:
		sign = "+"
	case s.spaceBeforePositive:
		sign = " "
	}

	nan, inf := math.IsNaN(val), math.IsInf(val, 0)

	switch s.ty {
	case typeString:
		if s.style != StylePrintf {
			return "", s.typeConversionErr("float", typeString)
		}
		fallthrough
	case typeDefault:
		switch {
		case nan:
			return s.formatNumber("nan", ""), nil
		case inf:
			return s.formatNumber("inf", sign), nil
		case val == 0:
			return s.formatNumber("0", sign), nil
		default:
			num := s.numberInGeneralFormat(math.Abs(val), false)
			if !strings.ContainsAny(num, ".eE") {
				num += ".0"
			}
			return s.formatNumber(num, sign), nil
		}
	case typeLowerE, typeUpperE:
		upper := s.ty == typeUpperE
		if nan {
			return s.formatNumber(pick(upper, "NAN", "nan"), ""), nil
		}
		if inf {
			return s.formatNumber(pick(upper, "INF", "inf"), sign), nil
		}
		precision := 6
		if s.hasPrecision {
			precision = s.precision
		}
		mant, exp := mantissaAndExp(math.Abs(val), precision)
		mant = s.groupDecimalNum(s.fixDecimalPoint(mant))
		return s.formatNumber(mant+pick(upper, "E", "e")+signedExp(exp), sign), nil
	case typeLowerF, typeUpperF:
		upper := s.ty == typeUpperF
		if nan {
			return s.formatNumber(pick(upper, "NAN", "nan"), ""), nil
		}
		if inf {
			return s.formatNumber(pick(upper, "INF", "inf"), sign), nil
		}
		precision := 6
		if s.hasPrecision {
			precision = s.precision
		}
		num := strconv.FormatFloat(math.Abs(val), 'f', precision, 64)
		return s.formatNumber(s.groupDecimalNum(s.fixDecimalPoint(num)), sign), nil
	case typeLowerG, typeUpperG:
		upper := s.ty == typeUpperG
		switch {
		case nan:
			return s.formatNumber(pick(upper, "NAN", "nan"), ""), nil
		case inf:
			return s.formatNumber(pick(upper, "INF", "inf"), sign), nil
		case val == 0:
			return s.formatNumber("0", sign), nil
		default:
			return s.formatNumber(s.numberInGeneralFormat(math.Abs(val), upper), sign), nil
		}
	default:
		return "", s.typeConversionErr("float", s.ty)
	}
}

func pick(cond bool, yes, no string) string {
	if cond {
		return yes
	}
	return no
}

// applyZeroPadding prepends '0's, keeping any digit grouping intact.
func (s *formatSpec) applyZeroPadding(num string, fillWidth int) string {
	var sep byte
	groupWidth := 3
	switch s.grouping {
	case ',':
		sep, groupWidth = ',', 3
	case '_':
		sep = '_'
		switch s.ty {
		case typeBinary, typeOctal, typeLowerHex, typeUpperHex:
			groupWidth = 4
		default:
			groupWidth = 3
		}
	default:
		return strings.Repeat("0", fillWidth) + num
	}

	firstSeparator := len(num)
	if point := strings.IndexByte(num, '.'); point >= 0 {
		if i := strings.IndexByte(num[:point], sep); i >= 0 {
			firstSeparator = i
		} else {
			firstSeparator = point
		}
	} else if i := strings.IndexByte(num, sep); i >= 0 {
		firstSeparator = i
	} else if i := strings.IndexAny(num, "eE"); i >= 0 {
		firstSeparator = i
	}

	prefix, groupedSuffix := num[:firstSeparator], num[firstSeparator:]
	groupedPrefix := group(strings.Repeat("0", fillWidth)+prefix, sep, groupWidth)
	trimIndex := len(groupedPrefix) - len(prefix) - fillWidth
	groupedPrefix = groupedPrefix[trimIndex:]
	lead := ""
	if strings.HasPrefix(groupedPrefix, string(sep)) {
		lead = "0"
	}
	return lead + groupedPrefix + groupedSuffix
}

func (s *formatSpec) formatNumber(number, sign string) string {
	radix := ""
	if s.alternateForm {
		switch s.ty {
		case typeBinary:
			radix = "0b"
		case typeOctal:
			radix = "0o"
		case typeLowerHex:
			radix = "0x"
		case typeUpperHex:
			radix = "0X"
		}
	}

	if s.zeroPadded {
		currWidth := len(sign) + len(radix) + len(number)
		if currWidth < s.width {
			return sign + radix + s.applyZeroPadding(number, s.width-currWidth)
		}
		return sign + radix + number
	}
	return s.applyPadding(sign+radix+number, alignRight)
}

func (s *formatSpec) applyPadding(text string, defaultAlign align) string {
	if !s.hasWidth || len(text) >= s.width {
		return text
	}
	fillWidth := s.width - len(text)
	fillChar, a := ' ', defaultAlign
	if s.hasFillAlign {
		a = s.fillAlign
		if s.hasFill {
			fillChar = s.fill
		}
	}
	switch a {
	case alignLeft:
		return text + strings.Repeat(string(fillChar), fillWidth)
	case alignRight:
		return strings.Repeat(string(fillChar), fillWidth) + text
	default:
		left := fillWidth / 2
		right := fillWidth - left
		return strings.Repeat(string(fillChar), left) + text + strings.Repeat(string(fillChar), right)
	}
}
