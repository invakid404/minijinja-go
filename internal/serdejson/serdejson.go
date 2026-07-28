// Package serdejson serializes fork values the way the engine does.
//
// The engine's `tojson` filter is `serde_json` (filters.rs:1007-1048), and Go's
// encoding/json is not a drop-in for it:
//
//   - encoding/json escapes U+2028 and U+2029; serde_json emits them raw.
//   - encoding/json prints an integral float as "2"; serde_json prints "2.0",
//     and switches to scientific notation at different magnitudes than Go's
//     %g does ("1e+16", "1e-6").
//   - the engine additionally escapes < > & and ' as \uXXXX after serializing,
//     so the result is safe to embed in HTML as well as in JSON.
//
// Nothing here is generic JSON: it reproduces one implementation's bytes.
package serdejson

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/invakid404/minijinja-go/v2/value"
)

// Marshal renders val as JSON. A negative indent is compact; zero or more is
// pretty printed with that many spaces, matching serde_json's PrettyFormatter.
func Marshal(val value.Value, indent int) (string, error) {
	var b strings.Builder
	e := encoder{buf: &b, indent: indent}
	if err := e.encode(val, 0); err != nil {
		return "", err
	}
	return b.String(), nil
}

type encoder struct {
	buf    *strings.Builder
	indent int
}

func (e *encoder) pretty() bool { return e.indent >= 0 }

func (e *encoder) newline(depth int) {
	if !e.pretty() {
		return
	}
	e.buf.WriteByte('\n')
	e.buf.WriteString(strings.Repeat(" ", e.indent*depth))
}

func (e *encoder) encode(val value.Value, depth int) error {
	// An invalid value serializes as null, as it does through serde.
	if _, ok := value.InvalidError(val); ok {
		e.buf.WriteString("null")
		return nil
	}
	switch val.Kind() {
	case value.KindUndefined, value.KindNone:
		e.buf.WriteString("null")
		return nil
	case value.KindBool:
		b, _ := val.AsBool()
		e.buf.WriteString(strconv.FormatBool(b))
		return nil
	case value.KindNumber:
		return e.encodeNumber(val)
	case value.KindString:
		s, _ := val.AsString()
		e.encodeString(s)
		return nil
	case value.KindSeq, value.KindIterable:
		return e.encodeSeq(val.Iter(), depth)
	case value.KindMap:
		return e.encodeMap(val, depth)
	case value.KindBytes:
		// serde serializes bytes as a sequence of numbers.
		raw, ok := val.Raw().([]byte)
		if !ok {
			return fmt.Errorf("cannot serialize to JSON")
		}
		items := make([]value.Value, len(raw))
		for i, c := range raw {
			items[i] = value.FromInt(int64(c))
		}
		return e.encodeSeq(items, depth)
	default:
		// A host object serializes through its map or sequence shape if it has
		// one — an imported template module is a map of its exports, and that
		// is how the engine serializes it too. Otherwise it is not
		// serializable.
		if m, ok := val.AsMap(); ok {
			names := make([]string, 0, len(m))
			for name := range m {
				names = append(names, name)
			}
			sort.Strings(names)
			return e.encodeEntries(names, func(name string) value.Value { return m[name] }, depth)
		}
		if obj, ok := val.AsObject(); ok {
			if m, ok := obj.(value.MapObject); ok {
				return e.encodeEntries(m.Keys(), val.GetAttr, depth)
			}
		}
		if items := val.Iter(); items != nil {
			return e.encodeSeq(items, depth)
		}
		return fmt.Errorf("cannot serialize to JSON")
	}
}

func (e *encoder) encodeSeq(items []value.Value, depth int) error {
	if len(items) == 0 {
		e.buf.WriteString("[]")
		return nil
	}
	e.buf.WriteByte('[')
	for i, item := range items {
		if i > 0 {
			e.buf.WriteByte(',')
		}
		e.newline(depth + 1)
		if err := e.encode(item, depth+1); err != nil {
			return err
		}
	}
	e.newline(depth)
	e.buf.WriteByte(']')
	return nil
}

func (e *encoder) encodeMap(val value.Value, depth int) error {
	keys := val.Iter()
	names := make([]string, len(keys))
	for i, key := range keys {
		name, ok := key.AsString()
		if !ok {
			name = key.String()
		}
		names[i] = name
	}
	return e.encodeEntries(names, func(name string) value.Value {
		return val.GetItem(value.FromString(name))
	}, depth)
}

func (e *encoder) encodeEntries(names []string, get func(string) value.Value, depth int) error {
	if len(names) == 0 {
		e.buf.WriteString("{}")
		return nil
	}
	e.buf.WriteByte('{')
	for i, name := range names {
		if i > 0 {
			e.buf.WriteByte(',')
		}
		e.newline(depth + 1)
		e.encodeString(name)
		e.buf.WriteByte(':')
		if e.pretty() {
			e.buf.WriteByte(' ')
		}
		if err := e.encode(get(name), depth+1); err != nil {
			return err
		}
	}
	e.newline(depth)
	e.buf.WriteByte('}')
	return nil
}

func (e *encoder) encodeNumber(val value.Value) error {
	if val.IsActualInt() {
		if n, ok := val.AsInt(); ok {
			e.buf.WriteString(strconv.FormatInt(n, 10))
			return nil
		}
		// A big integer keeps its own decimal rendering.
		e.buf.WriteString(val.String())
		return nil
	}
	f, ok := val.AsFloat()
	if !ok {
		return fmt.Errorf("cannot serialize to JSON")
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		// serde_json has no representation for these and emits null.
		e.buf.WriteString("null")
		return nil
	}
	e.buf.WriteString(FormatFloat(f))
	return nil
}

// FormatFloat renders a float the way serde_json does.
//
// The shortest round-tripping decimal is used, printed in fixed notation while
// the decimal exponent is in [-5, 15] — always with a fractional part, so an
// integral value is "2.0" and not "2" — and in scientific notation outside it,
// with a signed exponent and no zero padding: "1e+16", "1e-6", "5e-324".
func FormatFloat(f float64) string {
	sci := strconv.FormatFloat(f, 'e', -1, 64)
	mantissa, expPart, ok := strings.Cut(sci, "e")
	if !ok {
		return sci
	}
	exp, err := strconv.Atoi(expPart)
	if err != nil {
		return sci
	}
	if exp >= -5 && exp <= 15 {
		fixed := strconv.FormatFloat(f, 'f', -1, 64)
		if !strings.Contains(fixed, ".") {
			fixed += ".0"
		}
		return fixed
	}
	return fmt.Sprintf("%se%+d", mantissa, exp)
}

// escapes mirrors serde_json's ESCAPE table: the two structural characters and
// the C0 controls. Everything else — including DEL, U+2028 and U+2029 — is
// emitted as is.
func (e *encoder) encodeString(s string) {
	e.buf.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			e.buf.WriteString(`\"`)
		case c == '\\':
			e.buf.WriteString(`\\`)
		case c == '\b':
			e.buf.WriteString(`\b`)
		case c == '\f':
			e.buf.WriteString(`\f`)
		case c == '\n':
			e.buf.WriteString(`\n`)
		case c == '\r':
			e.buf.WriteString(`\r`)
		case c == '\t':
			e.buf.WriteString(`\t`)
		case c < 0x20:
			fmt.Fprintf(e.buf, `\u%04x`, c)
		default:
			e.buf.WriteByte(c)
		}
	}
	e.buf.WriteByte('"')
}

// EscapeForHTML applies the pass the `tojson` filter makes over the serialized
// text, which makes the result safe to embed in HTML as well as in JSON
// (filters.rs:1034-1046).
func EscapeForHTML(s string) string {
	if !strings.ContainsAny(s, "<>&'") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '<':
			b.WriteString("\\u003c")
		case '>':
			b.WriteString("\\u003e")
		case '&':
			b.WriteString("\\u0026")
		case '\'':
			b.WriteString("\\u0027")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
