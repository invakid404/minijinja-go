// Package pycompat implements Python methods on primitive values.
//
// It is the Go port of minijinja-contrib's `pycompat` module
// (minijinja-contrib/src/pycompat.rs:17-335 at boundaryml/minijinja@8cfc770),
// the unknown-method callback BAML installs on its environment
// (jinja_helpers.rs:34). Installing it makes `{{ "x".upper() }}` and
// `{{ mapping.items() }}` work the way a Jinja2 template expects:
//
//	env := minijinja.NewEnvironment()
//	env.SetUnknownMethodCallback(pycompat.UnknownMethodCallback)
//
// Nothing installs it automatically. It is a module a host chooses, exactly as
// it is in the Rust engine, and it is deliberately separate from the BAML
// profile: no regex_match, no sum, no none-formatter live here.
//
// The methods implemented are the reference module's, no more and no less:
//
//	dict:   get, items, keys, values
//	list:   count
//	str:    capitalize, count, endswith, find, format, isalnum, isalpha,
//	        isascii, isdigit, islower, isnumeric, isspace, isupper, join,
//	        lower, lstrip, replace, rfind, rstrip, split, splitlines,
//	        startswith, strip, title, upper
//
// Anything else declines with minijinja.ErrUnknownMethod, which is what lets
// the engine report its own error.
package pycompat

import (
	"fmt"
	"strings"

	minijinja "github.com/invakid404/minijinja-go/v2"
	"github.com/invakid404/minijinja-go/v2/filters"
	"github.com/invakid404/minijinja-go/v2/internal/pyformat"
	"github.com/invakid404/minijinja-go/v2/internal/unicodecase"
	"github.com/invakid404/minijinja-go/v2/value"
)

// UnknownMethodCallback implements Python methods on primitives.
//
// Install it with minijinja.Environment.SetUnknownMethodCallback. It is
// pycompat.rs:49-61: string, map and sequence values are routed to their
// method tables and everything else declines.
func UnknownMethodCallback(state *minijinja.State, val value.Value, method string, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
	// The engine hands keyword arguments to a method as one trailing Kwargs
	// value in the argument slice, and every method below consumes its
	// arguments with `from_args` (argtypes.rs:190-240). Modelling them the
	// same way is what makes `"x".upper(a=1)` too many arguments while
	// `"x".find(a=1)` is a non-string argument — two different errors, as in
	// the reference implementation. `str.format` is the one method that reads
	// them, so it receives them separately.
	if len(kwargs) > 0 && method != "format" {
		args = append(append([]value.Value(nil), args...), value.FromMap(kwargs))
	}

	switch val.Kind() {
	case value.KindString:
		return stringMethods(val, method, args, kwargs)
	case value.KindMap:
		return mapMethods(val, method, args)
	case value.KindSeq:
		return seqMethods(val, method, args)
	default:
		return value.Undefined(), unknownMethod()
	}
}

func unknownMethod() error {
	return minijinja.NewError(minijinja.ErrUnknownMethod, "unknown method")
}

func tooManyArguments() error {
	return minijinja.NewError(minijinja.ErrTooManyArguments, "received too many arguments")
}

func missingArgument() error {
	return minijinja.NewError(minijinja.ErrMissingArgument, "missing argument")
}

func notAString() error {
	return minijinja.NewError(minijinja.ErrInvalidOperation, "value is not a string")
}

// noArgs is `let () = from_args(args)?`.
func noArgs(args []value.Value) error {
	if len(args) > 0 {
		return tooManyArguments()
	}
	return nil
}

// argString is the `&str` argument type: a missing argument and a non-string
// argument are different errors (argtypes.rs:469-480).
func argString(args []value.Value, i int) (string, error) {
	if i >= len(args) {
		return "", missingArgument()
	}
	s, ok := args[i].AsString()
	if !ok {
		return "", notAString()
	}
	return s, nil
}

// optString is `Option<&str>`: absent is fine, present but not a string is not.
func optString(args []value.Value, i int) (string, bool, error) {
	if i >= len(args) {
		return "", false, nil
	}
	// `Option<T>` maps none/undefined to None before consulting T
	// (argtypes.rs:390-402), which is what makes `split(none, 1)` work.
	if args[i].IsNone() || args[i].IsUndefined() {
		return "", false, nil
	}
	s, ok := args[i].AsString()
	if !ok {
		return "", false, notAString()
	}
	return s, true, nil
}

// optInt is `Option<i32>`/`Option<i64>`.
func optInt(args []value.Value, i int) (int64, bool, error) {
	if i >= len(args) {
		return 0, false, nil
	}
	if args[i].IsNone() || args[i].IsUndefined() {
		return 0, false, nil
	}
	n, ok := args[i].AsInt()
	if !ok || !args[i].IsActualInt() {
		return 0, false, minijinja.NewError(minijinja.ErrInvalidOperation,
			fmt.Sprintf("cannot convert %s to i64", args[i].Kind()))
	}
	return n, true, nil
}

// optBool is `Option<bool>`.
func optBool(args []value.Value, i int) (bool, error) {
	if i >= len(args) {
		return false, nil
	}
	if args[i].IsNone() || args[i].IsUndefined() {
		return false, nil
	}
	b, ok := args[i].AsBool()
	if !ok {
		return false, minijinja.NewError(minijinja.ErrInvalidOperation,
			fmt.Sprintf("cannot convert %s to bool", args[i].Kind()))
	}
	return b, nil
}

func allRunes(s string, pred func(rune) bool) value.Value {
	for _, r := range s {
		if !pred(r) {
			return value.FromBool(false)
		}
	}
	return value.FromBool(true)
}

func stringMethods(val value.Value, method string, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
	s, ok := val.AsString()
	if !ok {
		return value.Undefined(), unknownMethod()
	}

	switch method {
	case "upper":
		if err := noArgs(args); err != nil {
			return value.Undefined(), err
		}
		return value.FromString(unicodecase.ToUpper(s)), nil

	case "lower":
		if err := noArgs(args); err != nil {
			return value.Undefined(), err
		}
		return value.FromString(unicodecase.ToLower(s)), nil

	case "islower":
		if err := noArgs(args); err != nil {
			return value.Undefined(), err
		}
		return allRunes(s, unicodecase.IsLower), nil

	case "isupper":
		if err := noArgs(args); err != nil {
			return value.Undefined(), err
		}
		return allRunes(s, unicodecase.IsUpper), nil

	case "isspace":
		if err := noArgs(args); err != nil {
			return value.Undefined(), err
		}
		return allRunes(s, unicodecase.IsSpace), nil

	case "isdigit", "isnumeric":
		if err := noArgs(args); err != nil {
			return value.Undefined(), err
		}
		return allRunes(s, unicodecase.IsNumeric), nil

	case "isalnum":
		if err := noArgs(args); err != nil {
			return value.Undefined(), err
		}
		return allRunes(s, unicodecase.IsAlphanumeric), nil

	case "isalpha":
		if err := noArgs(args); err != nil {
			return value.Undefined(), err
		}
		return allRunes(s, unicodecase.IsAlphabetic), nil

	case "isascii":
		if err := noArgs(args); err != nil {
			return value.Undefined(), err
		}
		for i := 0; i < len(s); i++ {
			if s[i] >= 0x80 {
				return value.FromBool(false), nil
			}
		}
		return value.FromBool(true), nil

	case "strip", "lstrip", "rstrip":
		chars, given, err := optString(args, 0)
		if err != nil {
			return value.Undefined(), err
		}
		if err := noArgs(args[min(1, len(args)):]); err != nil {
			return value.Undefined(), err
		}
		switch {
		case method == "strip" && given:
			return value.FromString(strings.Trim(s, chars)), nil
		case method == "strip":
			return value.FromString(unicodecase.TrimSpace(s)), nil
		case method == "lstrip" && given:
			return value.FromString(strings.TrimLeft(s, chars)), nil
		case method == "lstrip":
			return value.FromString(unicodecase.TrimLeftSpace(s)), nil
		case given:
			return value.FromString(strings.TrimRight(s, chars)), nil
		default:
			return value.FromString(unicodecase.TrimRightSpace(s)), nil
		}

	case "replace":
		old, err := argString(args, 0)
		if err != nil {
			return value.Undefined(), err
		}
		new, err := argString(args, 1)
		if err != nil {
			return value.Undefined(), err
		}
		count, given, err := optInt(args, 2)
		if err != nil {
			return value.Undefined(), err
		}
		if err := noArgs(args[min(3, len(args)):]); err != nil {
			return value.Undefined(), err
		}
		if !given {
			count = -1
		}
		if count < 0 {
			return value.FromString(strings.Replace(s, old, new, -1)), nil
		}
		return value.FromString(strings.Replace(s, old, new, int(count))), nil

	case "title":
		if err := noArgs(args); err != nil {
			return value.Undefined(), err
		}
		return value.FromString(filters.Title(s)), nil

	case "capitalize":
		if err := noArgs(args); err != nil {
			return value.Undefined(), err
		}
		return value.FromString(filters.Capitalize(s)), nil

	case "split":
		sep, hasSep, err := optString(args, 0)
		if err != nil {
			return value.Undefined(), err
		}
		maxSplits, hasMax, err := optInt(args, 1)
		if err != nil {
			return value.Undefined(), err
		}
		if err := noArgs(args[min(2, len(args)):]); err != nil {
			return value.Undefined(), err
		}
		parts := filters.Split(s, sep, hasSep, maxSplits, hasMax)
		items := make([]value.Value, 0, len(parts))
		for _, part := range parts {
			items = append(items, value.FromString(part))
		}
		return value.FromSlice(items), nil

	case "splitlines":
		keepEnds, err := optBool(args, 0)
		if err != nil {
			return value.Undefined(), err
		}
		if err := noArgs(args[min(1, len(args)):]); err != nil {
			return value.Undefined(), err
		}
		var items []value.Value
		if !keepEnds {
			for _, line := range splitLines(s) {
				items = append(items, value.FromString(line))
			}
			return value.FromSlice(items), nil
		}
		rest := s
		for {
			offset := strings.IndexByte(rest, '\n')
			if offset < 0 {
				break
			}
			items = append(items, value.FromString(rest[:offset+1]))
			rest = rest[offset+1:]
		}
		if rest != "" {
			items = append(items, value.FromString(rest))
		}
		return value.FromSlice(items), nil

	case "count":
		what, err := argString(args, 0)
		if err != nil {
			return value.Undefined(), err
		}
		if err := noArgs(args[min(1, len(args)):]); err != nil {
			return value.Undefined(), err
		}
		return value.FromInt(countOccurrences(s, what)), nil

	case "find":
		what, err := argString(args, 0)
		if err != nil {
			return value.Undefined(), err
		}
		if err := noArgs(args[min(1, len(args)):]); err != nil {
			return value.Undefined(), err
		}
		// A BYTE offset, as in Rust's str::find, not a character index.
		return value.FromInt(int64(strings.Index(s, what))), nil

	case "rfind":
		what, err := argString(args, 0)
		if err != nil {
			return value.Undefined(), err
		}
		if err := noArgs(args[min(1, len(args)):]); err != nil {
			return value.Undefined(), err
		}
		return value.FromInt(int64(strings.LastIndex(s, what))), nil

	case "format":
		out, err := pyformat.Format(pyformat.StyleStrFormat, s, args, kwargs)
		if err != nil {
			return value.Undefined(), err
		}
		return value.FromString(out), nil

	case "startswith", "endswith":
		if len(args) == 0 {
			return value.Undefined(), missingArgument()
		}
		if err := noArgs(args[1:]); err != nil {
			return value.Undefined(), err
		}
		return affixTest(s, method, args[0])

	case "join":
		if len(args) == 0 {
			return value.Undefined(), missingArgument()
		}
		if err := noArgs(args[1:]); err != nil {
			return value.Undefined(), err
		}
		var b strings.Builder
		for idx, item := range args[0].Iter() {
			if idx > 0 {
				b.WriteString(s)
			}
			b.WriteString(item.String())
		}
		return value.FromString(b.String()), nil

	default:
		return value.Undefined(), unknownMethod()
	}
}

// affixTest is startswith/endswith: a string, or an iterable of strings any of
// which may match (pycompat.rs:202-259).
func affixTest(s, method string, arg value.Value) (value.Value, error) {
	match := strings.HasPrefix
	if method == "endswith" {
		match = strings.HasSuffix
	}
	if affix, ok := arg.AsString(); ok {
		return value.FromBool(match(s, affix)), nil
	}
	switch arg.Kind() {
	case value.KindSeq, value.KindIterable:
		for _, item := range arg.Iter() {
			affix, ok := item.AsString()
			if !ok {
				return value.Undefined(), minijinja.NewError(minijinja.ErrInvalidOperation,
					fmt.Sprintf("tuple for %s must contain only strings, not %s", method, item.Kind()))
			}
			if match(s, affix) {
				return value.FromBool(true), nil
			}
		}
		return value.FromBool(false), nil
	default:
		return value.Undefined(), minijinja.NewError(minijinja.ErrInvalidOperation,
			fmt.Sprintf("%s argument must be string or a tuple of strings, not %s", method, arg.Kind()))
	}
}

// countOccurrences counts non-overlapping occurrences of what in s.
//
// The reference implementation walks the string with `find` and advances by
// the needle's length (pycompat.rs:177-186). With an empty needle that never
// advances and the engine loops forever; this returns Python's answer instead
// of hanging. It is the one place in this package that deliberately does not
// reproduce the reference behaviour — see PATCHES.md.
func countOccurrences(s, what string) int64 {
	if what == "" {
		return int64(len([]rune(s)) + 1)
	}
	var count int64
	rest := s
	for {
		offset := strings.Index(rest, what)
		if offset < 0 {
			return count
		}
		count++
		rest = rest[offset+len(what):]
	}
}

// splitLines is str::lines: split on '\n', dropping one trailing '\r', with no
// empty final element for a trailing newline.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	rest := s
	for {
		offset := strings.IndexByte(rest, '\n')
		if offset < 0 {
			break
		}
		out = append(out, strings.TrimSuffix(rest[:offset], "\r"))
		rest = rest[offset+1:]
	}
	if rest != "" {
		out = append(out, rest)
	}
	return out
}

func mapMethods(val value.Value, method string, args []value.Value) (value.Value, error) {
	m, ok := val.AsMap()
	if !ok {
		return value.Undefined(), unknownMethod()
	}

	switch method {
	case "keys", "values", "items":
		if err := noArgs(args); err != nil {
			return value.Undefined(), err
		}
		// Iteration order is the fork's map iteration order, the same one
		// `|items` and `{% for %}` use, so a mapping renders consistently
		// however it is walked.
		keys := val.Iter()
		out := make([]value.Value, 0, len(keys))
		for _, k := range keys {
			name, _ := k.AsString()
			switch method {
			case "keys":
				out = append(out, k)
			case "values":
				out = append(out, m[name])
			default:
				out = append(out, value.FromSlice([]value.Value{k, m[name]}))
			}
		}
		return value.FromSlice(out), nil

	case "get":
		if len(args) == 0 {
			return value.Undefined(), missingArgument()
		}
		if err := noArgs(args[min(2, len(args)):]); err != nil {
			return value.Undefined(), err
		}
		if got := val.GetItem(args[0]); !got.IsUndefined() {
			return got, nil
		}
		if len(args) > 1 {
			return args[1], nil
		}
		// Python's dict.get returns None, not undefined.
		return value.None(), nil

	default:
		return value.Undefined(), unknownMethod()
	}
}

func seqMethods(val value.Value, method string, args []value.Value) (value.Value, error) {
	switch method {
	case "count":
		if len(args) == 0 {
			return value.Undefined(), missingArgument()
		}
		if err := noArgs(args[1:]); err != nil {
			return value.Undefined(), err
		}
		var count int64
		for _, item := range val.Iter() {
			if item.Equal(args[0]) {
				count++
			}
		}
		return value.FromInt(count), nil

	default:
		return value.Undefined(), unknownMethod()
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
