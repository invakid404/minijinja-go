package pyformat

import (
	"strconv"
	"unicode/utf8"
)

// pathElem is one step of a str.format field path: `.attr` or `[key]`.
type pathElem struct {
	name string
	attr bool
}

// fieldName is which argument a replacement field names. At most one of the
// three pointers is set; all nil means "the next positional argument".
type fieldName struct {
	kwarg      *string
	positional *int
	mappingKey *string
	path       []pathElem
}

type token struct {
	isLiteral bool
	literal   string
	field     fieldName
	spec      formatSpec
	location  int
}

// parser is the tokenizer over a format string, plus the cursor the
// style-specific field parsers advance.
type parser struct {
	src   string
	pos   int
	style Style
}

func (p *parser) rest() string { return p.src[p.pos:] }

func (p *parser) advance(n int) string {
	consumed := p.src[p.pos : p.pos+n]
	p.pos += n
	return consumed
}

func (p *parser) advanceIf(c byte) bool {
	if p.pos < len(p.src) && p.src[p.pos] == c {
		p.pos++
		return true
	}
	return false
}

func (p *parser) atEnd() bool { return p.pos == len(p.src) }

// next returns the next literal or replacement field, or nil at the end.
func (p *parser) next() (*token, error) {
	delimiter := byte('%')
	if p.style == StyleStrFormat {
		delimiter = '{'
	}

	rest := p.rest()
	offset := 0
	foundSpec := false
	escapeSeq := false
	for {
		if offset >= len(rest) {
			break
		}
		c := rest[offset]
		switch {
		case c == delimiter:
			if offset+1 < len(rest) && rest[offset+1] == delimiter {
				// "%%" / "{{": the first character joins the literal and the
				// second is consumed below.
				escapeSeq = true
				offset++
			} else {
				foundSpec = true
			}
		case c == '}' && p.style == StyleStrFormat:
			if offset+1 < len(rest) && rest[offset+1] == '}' {
				escapeSeq = true
				offset++
			} else {
				return nil, invalidOp(
					"invalid single '}' in format string at offset %d; use escape sequence '}}'", offset)
			}
		default:
			offset++
			continue
		}
		break
	}

	switch {
	case offset > 0:
		tok := &token{isLiteral: true, literal: p.advance(offset)}
		if escapeSeq {
			p.advance(1)
		}
		return tok, nil
	case foundSpec:
		if p.style == StylePrintf {
			return p.printfField()
		}
		return p.strFormatField()
	default:
		return nil, nil
	}
}

// parseNumber consumes a run of ASCII digits.
func (p *parser) parseNumber() (int, bool, error) {
	digits := 0
	for digits < len(p.rest()) && p.rest()[digits] >= '0' && p.rest()[digits] <= '9' {
		digits++
	}
	if digits == 0 {
		return 0, false, nil
	}
	text := p.advance(digits)
	n, err := strconv.Atoi(text)
	if err != nil || n < 0 {
		return 0, false, invalidOp("invalid integer in the format string at offset %d", p.pos)
	}
	return n, true, nil
}

func (p *parser) parseType() (convType, error) {
	if p.atEnd() {
		return typeDefault, invalidOp(
			"incomplete format spec at offset %d; missing conversion type", p.pos)
	}
	var ty convType
	switch c := p.src[p.pos]; {
	case c == 'b' && p.style == StyleStrFormat:
		ty = typeBinary
	case c == 'd':
		ty = typeDecimal
	case c == 'i' && p.style == StylePrintf:
		ty = typeDecimal
	case c == 'e':
		ty = typeLowerE
	case c == 'E':
		ty = typeUpperE
	case c == 'f':
		ty = typeLowerF
	case c == 'F':
		ty = typeUpperF
	case c == 'g':
		ty = typeLowerG
	case c == 'G':
		ty = typeUpperG
	case c == 'o':
		ty = typeOctal
	case c == 'x':
		ty = typeLowerHex
	case c == 'X':
		ty = typeUpperHex
	case c == 's':
		ty = typeString
	case c == '}' && p.style == StyleStrFormat:
		// End of the spec: the '}' is left for the caller.
		return typeDefault, nil
	default:
		return typeDefault, invalidOp(
			"invalid conversion type '%c' in format spec at offset %d", c, p.pos)
	}
	p.advance(1)
	return ty, nil
}

// parseTill consumes up to and including endDelim, returning what precedes it.
func (p *parser) parseTill(endDelim byte) (string, error) {
	start := p.pos
	for {
		if p.advanceIf(endDelim) {
			break
		}
		if p.atEnd() {
			return "", invalidOp("incomplete format key at offset %d; missing closing '%c'",
				start, endDelim)
		}
		p.advance(1)
	}
	return p.src[start : p.pos-1], nil
}

// --- printf style ----------------------------------------------------------

func (p *parser) printfField() (*token, error) {
	location := p.pos
	p.advance(1) // '%'

	var field fieldName
	if p.advanceIf('(') {
		key, err := p.parseTill(')')
		if err != nil {
			return nil, err
		}
		field.mappingKey = &key
	}

	spec, err := p.printfSpec()
	if err != nil {
		return nil, err
	}
	return &token{field: field, spec: spec, location: location}, nil
}

func (p *parser) printfSpec() (formatSpec, error) {
	spec := formatSpec{style: StylePrintf, location: p.pos}

	for !p.atEnd() {
		switch p.src[p.pos] {
		case '#':
			spec.alternateForm = true
		case '0':
			spec.zeroPadded = true
		case '-':
			spec.hasFillAlign = true
			spec.fillAlign = alignLeft
		case ' ':
			spec.spaceBeforePositive = true
		case '+':
			spec.printSign = true
		default:
			goto flagsDone
		}
		p.advance(1)
	}
flagsDone:

	if spec.printSign {
		// '+' overrides ' '.
		spec.spaceBeforePositive = false
	}
	if spec.hasFillAlign && spec.fillAlign == alignLeft {
		// '-' overrides '0'.
		spec.zeroPadded = false
	}

	width, hasWidth, err := p.parseNumber()
	if err != nil {
		return spec, err
	}
	spec.width, spec.hasWidth = width, hasWidth
	if spec.zeroPadded && !hasWidth {
		// A '0' with no digits after it was the width, not a padding flag.
		spec.zeroPadded = false
		spec.width, spec.hasWidth = 0, true
	}

	if p.advanceIf('.') {
		precision, hasPrecision, err := p.parseNumber()
		if err != nil {
			return spec, err
		}
		spec.precision, spec.hasPrecision = precision, hasPrecision
	}

	// Length modifiers are parsed and ignored, as in Python.
	if !p.atEnd() {
		switch p.src[p.pos] {
		case 'h', 'l', 'L':
			p.advance(1)
		}
	}

	ty, err := p.parseType()
	if err != nil {
		return spec, err
	}
	spec.ty = ty
	return spec, nil
}

// --- str.format style ------------------------------------------------------

func (p *parser) strFormatField() (*token, error) {
	location := p.pos
	p.advance(1) // '{'

	field, err := p.parseFieldName()
	if err != nil {
		return nil, err
	}

	spec := formatSpec{style: StyleStrFormat, location: location}
	if p.advanceIf(':') {
		spec, err = p.strFormatSpec()
		if err != nil {
			return nil, err
		}
	}

	if !p.advanceIf('}') {
		if !p.atEnd() {
			return nil, invalidOp("expected closing '}' in format spec at offset %d; found '%c'",
				location, p.src[p.pos])
		}
		return nil, invalidOp("missing closing '}' in format spec at offset %d", location)
	}
	return &token{field: field, spec: spec, location: location}, nil
}

func (p *parser) parseFieldName() (fieldName, error) {
	var field fieldName
	if num, ok, err := p.parseNumber(); err != nil {
		return field, err
	} else if ok {
		field.positional = &num
		path, err := p.parsePath()
		if err != nil {
			return field, err
		}
		field.path = path
		return field, nil
	}
	if ident, ok := p.parseIdentifier(); ok {
		field.kwarg = &ident
		path, err := p.parsePath()
		if err != nil {
			return field, err
		}
		field.path = path
	}
	return field, nil
}

func (p *parser) parsePath() ([]pathElem, error) {
	var elems []pathElem
	for {
		switch {
		case p.advanceIf('.'):
			attr, ok := p.parseIdentifier()
			if !ok {
				return nil, invalidOp(
					"missing attribute name after '.' in format spec at offset %d", p.pos)
			}
			elems = append(elems, pathElem{name: attr, attr: true})
		case p.advanceIf('['):
			key, err := p.parseTill(']')
			if err != nil {
				return nil, err
			}
			elems = append(elems, pathElem{name: key})
		default:
			return elems, nil
		}
	}
}

func (p *parser) parseIdentifier() (string, bool) {
	rest := p.rest()
	n := 0
	for n < len(rest) {
		c := rest[n]
		switch {
		case c == '_':
		case n == 0 && isASCIIAlpha(c):
		case n > 0 && (isASCIIAlpha(c) || (c >= '0' && c <= '9')):
		default:
			goto done
		}
		n++
	}
done:
	if n == 0 {
		return "", false
	}
	return p.advance(n), true
}

func isASCIIAlpha(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func (p *parser) strFormatSpec() (formatSpec, error) {
	spec := formatSpec{style: StyleStrFormat, location: p.pos}
	p.parseFillAlign(&spec)

	switch {
	case p.advanceIf('+'):
		spec.printSign = true
	case p.advanceIf(' '):
		spec.spaceBeforePositive = true
	default:
		p.advanceIf('-')
	}

	spec.alternateForm = p.advanceIf('#')
	spec.zeroPadded = p.advanceIf('0')

	width, hasWidth, err := p.parseNumber()
	if err != nil {
		return spec, err
	}
	spec.width, spec.hasWidth = width, hasWidth
	if spec.zeroPadded && !hasWidth {
		spec.zeroPadded = false
		spec.width, spec.hasWidth = 0, true
	}

	switch {
	case p.advanceIf(','):
		spec.grouping = ','
	case p.advanceIf('_'):
		spec.grouping = '_'
	}

	if p.advanceIf('.') {
		precision, hasPrecision, err := p.parseNumber()
		if err != nil {
			return spec, err
		}
		spec.precision, spec.hasPrecision = precision, hasPrecision
	}

	ty, err := p.parseType()
	if err != nil {
		return spec, err
	}
	spec.ty = ty
	return spec, nil
}

// parseFillAlign reads `[[fill]align]`, where fill is any character and align
// is one of `<`, `>`, `^`.
func (p *parser) parseFillAlign(spec *formatSpec) {
	rest := p.rest()
	if rest == "" {
		return
	}
	first, firstSize := utf8.DecodeRuneInString(rest)
	second, _ := utf8.DecodeRuneInString(rest[firstSize:])

	if a, ok := alignOf(second); ok {
		spec.hasFillAlign = true
		spec.hasFill = true
		spec.fill = first
		spec.fillAlign = a
		p.advance(firstSize + 1)
		return
	}
	if a, ok := alignOf(first); ok {
		spec.hasFillAlign = true
		spec.fillAlign = a
		p.advance(1)
	}
}

func alignOf(r rune) (align, bool) {
	switch r {
	case '<':
		return alignLeft, true
	case '>':
		return alignRight, true
	case '^':
		return alignCenter, true
	default:
		return alignLeft, false
	}
}
