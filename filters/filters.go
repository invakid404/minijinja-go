// Package filters provides MiniJinja's built-in filters.
package filters

import (
	"context"
	"fmt"
	"iter"
	"math"
	"math/big"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	mjerrors "github.com/invakid404/minijinja-go/v2/internal/errors"
	"github.com/invakid404/minijinja-go/v2/internal/pyformat"
	"github.com/invakid404/minijinja-go/v2/internal/serdejson"
	"github.com/invakid404/minijinja-go/v2/internal/unicodecase"
	"github.com/invakid404/minijinja-go/v2/value"
)

// State provides access to the runtime context for filters and tests.
//
// It is implemented by *minijinja.State and exposes helper methods for
// looking up filters and tests by name.
type State interface {
	Context() context.Context
	Lookup(name string) value.Value
	Name() string
	GetFilter(name string) (FilterFunc, bool)
	GetTest(name string) (TestFunc, bool)
}

// FilterFunc is the signature for filter functions.
type FilterFunc func(state State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error)

// TestFunc is the signature for test functions.
type TestFunc func(state State, val value.Value, args []value.Value) (bool, error)

type undefinedBehaviorProvider interface {
	UndefinedBehavior() value.UndefinedBehavior
}

func undefinedBehavior(state State) value.UndefinedBehavior {
	if state == nil {
		return value.UndefinedLenient
	}
	if provider, ok := state.(undefinedBehaviorProvider); ok {
		return provider.UndefinedBehavior()
	}
	return value.UndefinedLenient
}

// FilterUpper converts a value to uppercase.
//
// This filter converts the entire string to uppercase characters.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("upper", FilterUpper)
//
// Template usage:
//
//	<h1>{{ chapter.title|upper }}</h1>
//
// The engine's argument type is `Cow<'_, str>`, which accepts any value and
// stringifies a non-string one (argtypes.rs:547-568), so `{{ none|upper }}` is
// "NONE" rather than a type error.
//
// The mapping is Rust's full case mapping, not Go's simple one: "ß"
// uppercases to "SS". See internal/unicodecase.
func FilterUpper(_ State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	if err := noArgs(args, kwargs); err != nil {
		return value.Undefined(), err
	}
	s, err := stringArg(val)
	if err != nil {
		return value.Undefined(), err
	}
	return value.FromString(unicodecase.ToUpper(s)), nil
}

// stringArg is the engine's `Cow<'_, str>` argument type: a string is taken as
// is and anything else is stringified (argtypes.rs:547-568).
func stringArg(val value.Value) (string, error) {
	if s, ok := val.AsString(); ok {
		return s, nil
	}
	return val.String(), nil
}

// noArgs rejects any argument, the way a filter whose Rust signature takes
// only the value does (argtypes.rs:230-238).
func noArgs(args []value.Value, kwargs *value.OrderedMap) error {
	if len(args) > 0 || kwargs.Len() > 0 {
		return mjerrors.NewError(mjerrors.ErrTooManyArguments, "too many arguments")
	}
	return nil
}

// maxArgs rejects more than n positional arguments.
func maxArgs(args []value.Value, kwargs *value.OrderedMap, n int) error {
	if len(args) > n || kwargs.Len() > 0 {
		return mjerrors.NewError(mjerrors.ErrTooManyArguments, "too many arguments")
	}
	return nil
}

// tryIter is the engine's `undefined_behavior().try_iter()`.
//
// Sequences, maps, strings and iterators iterate; none and undefined iterate as
// EMPTY rather than failing, which is why `{{ none|list }}` is `[]`; anything
// else is an invalid operation. Answering something for a value the engine
// refuses is the dangerous direction, so every consumer of this reports the
// error rather than falling back to a default.
//
// A MAPPING is asked rather than assumed. `try_iter` is `enumerate()`, not
// `repr()` (value/object.rs:361-379), so a host object that is a map by
// REPRESENTATION but returns `Enumerator::NonEnumerable` does not iterate at
// all — it is not an empty mapping. [value.Value.Iter] answers nil for exactly
// that shape, and the mapping arm therefore goes through the same check the
// other unknown kinds do; see [value.Value.MapKeys] for the same distinction on
// the comparison side.
func tryIter(val value.Value, msg string) ([]value.Value, error) {
	switch val.Kind() {
	case value.KindSeq, value.KindIterable, value.KindString:
		return val.Iter(), nil
	case value.KindNone, value.KindUndefined:
		return nil, nil
	default:
		if items := val.Iter(); items != nil {
			return items, nil
		}
		return nil, mjerrors.NewError(mjerrors.ErrInvalidOperation, msg)
	}
}

// iterArg is tryIter with the engine's own message. Every filter that walks
// its subject goes through it, so a non-iterable is an invalid operation
// rather than a silent pass-through.
func iterArg(val value.Value) ([]value.Value, error) {
	return tryIter(val, fmt.Sprintf("%s is not iterable", val.Kind()))
}

// listArg is iterArg for the filters whose subject is a `Vec<Value>` PARAMETER
// rather than something they iterate themselves (`min`, `max`, `sort` —
// filters.rs:246-305). The engine reports the ArgType conversion there, not the
// iteration, and the two messages differ.
func listArg(val value.Value) ([]value.Value, error) {
	return tryIter(val, "cannot convert value to list")
}

// isIterable reports whether the engine can iterate a value at all. It is the
// predicate behind the `iterable` test, and none and undefined satisfy it.
func isIterable(val value.Value) bool {
	_, err := tryIter(val, "")
	return err == nil
}

// FilterLower converts a value to lowercase.
//
// This filter converts the entire string to lowercase characters.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("lower", FilterLower)
//
// Template usage:
//
//	<h1>{{ chapter.title|lower }}</h1>
//
// Like FilterUpper this is Rust's full case mapping, including the Greek
// final-sigma rule: "ΑΣ" lowercases to "ας".
func FilterLower(_ State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	if err := noArgs(args, kwargs); err != nil {
		return value.Undefined(), err
	}
	s, err := stringArg(val)
	if err != nil {
		return value.Undefined(), err
	}
	return value.FromString(unicodecase.ToLower(s)), nil
}

// FilterCapitalize converts the first character to uppercase and the rest to lowercase.
//
// This filter converts a string by uppercasing only the first character and
// lowercasing all remaining characters. This is different from FilterTitle which
// capitalizes the first letter of each word.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("capitalize", FilterCapitalize)
//
// Template usage:
//
//	{{ "hello WORLD"|capitalize }}
//	  -> "Hello world"
func FilterCapitalize(_ State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	if err := noArgs(args, kwargs); err != nil {
		return value.Undefined(), err
	}
	s, err := stringArg(val)
	if err != nil {
		return value.Undefined(), err
	}
	return value.FromString(Capitalize(s)), nil
}

// Capitalize is the `capitalize` filter's transform, shared with pycompat's
// str.capitalize (filters.rs:241-247): the first character uppercased with the
// full mapping, the rest lowercased.
func Capitalize(s string) string {
	if s == "" {
		return ""
	}
	first, size := utf8.DecodeRuneInString(s)
	return unicodecase.ToUpperRune(first) + unicodecase.ToLower(s[size:])
}

// FilterTitle converts a value to title case.
//
// This filter converts a string to title case by capitalizing the first letter
// of each word and lowercasing all other letters. Words are defined as sequences
// of characters separated by whitespace or common punctuation.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("title", FilterTitle)
//
// Template usage:
//
//	<h1>{{ chapter.title|title }}</h1>
//	{{ "hello world"|title }}
//	  -> "Hello World"
func FilterTitle(_ State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	if err := noArgs(args, kwargs); err != nil {
		return value.Undefined(), err
	}
	s, err := stringArg(val)
	if err != nil {
		return value.Undefined(), err
	}
	return value.FromString(Title(s)), nil
}

// Title is the `title` filter's transform, shared with pycompat's str.title
// (filters.rs:217-232).
//
// A word starts after ANY ASCII punctuation or any Unicode whitespace, which
// is a wider rule than "whitespace and a few separators": "a!b" titles to
// "A!B".
func Title(s string) string {
	var b strings.Builder
	capitalizeNext := true
	for _, r := range s {
		switch {
		case isASCIIPunctuation(r) || unicodecase.IsSpace(r):
			b.WriteRune(r)
			capitalizeNext = true
		case capitalizeNext:
			b.WriteString(unicodecase.ToUpperRune(r))
			capitalizeNext = false
		default:
			b.WriteString(unicodecase.ToLowerRune(r))
		}
	}
	return b.String()
}

// isASCIIPunctuation is Rust's char::is_ascii_punctuation: the ASCII graphic
// characters that are neither letters nor digits.
func isASCIIPunctuation(r rune) bool {
	switch {
	case r >= '!' && r <= '/':
		return true
	case r >= ':' && r <= '@':
		return true
	case r >= '[' && r <= '`':
		return true
	case r >= '{' && r <= '~':
		return true
	default:
		return false
	}
}

// FilterTrim strips leading and trailing characters from a string.
//
// By default, it strips whitespace characters. You can optionally provide
// a string of characters to trim as the first argument.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("trim", FilterTrim)
//
// Template usage:
//
//	{{ "  hello  "|trim }}
//	  -> "hello"
//	{{ "xxxhelloxxx"|trim("x") }}
//	  -> "hello"
//
// Without an argument this trims Unicode whitespace (Rust's str::trim), not
// the four ASCII space characters: a no-break space is trimmed too.
func FilterTrim(_ State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	s, err := stringArg(val)
	if err != nil {
		return value.Undefined(), err
	}
	a := NewArgs(args, kwargs)
	chars, given, err := a.OptCoerceStr()
	if err != nil {
		return value.Undefined(), err
	}
	if err := a.Done(); err != nil {
		return value.Undefined(), err
	}
	if given {
		return value.FromString(strings.Trim(s, chars)), nil
	}
	return value.FromString(unicodecase.TrimSpace(s)), nil
}

// FilterReplace replaces occurrences of a substring with another string.
//
// This filter replaces all occurrences of the first parameter with the second.
// Optionally, you can provide a third parameter to limit the number of replacements.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("replace", FilterReplace)
//
// Template usage:
//
//	{{ "Hello World"|replace("Hello", "Goodbye") }}
//	  -> "Goodbye World"
//	{{ "aaa"|replace("a", "b", 2) }}
//	  -> "bba"
//
// The engine's `replace` takes exactly two arguments (filters.rs:258-265).
// Jinja2's optional count is NOT part of it, so a third argument is an error
// rather than a silently supported extension.
func FilterReplace(_ State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	s, err := stringArg(val)
	if err != nil {
		return value.Undefined(), err
	}
	// Both parameters are `Cow<'_, str>`, so a keyword argument occupies the
	// next one and fails as a string conversion rather than as an arity error:
	// `"ab"|replace(nope=1)` is an invalid operation, not too many arguments.
	a := NewArgs(args, kwargs)
	from, err := a.CoerceStr()
	if err != nil {
		return value.Undefined(), err
	}
	to, err := a.CoerceStr()
	if err != nil {
		return value.Undefined(), err
	}
	if err := a.Done(); err != nil {
		return value.Undefined(), err
	}
	return value.FromString(strings.ReplaceAll(s, from, to)), nil
}

// FilterFormat applies printf-style formatting to a string.
//
// Example:
//
//	{{ "%s, %s!"|format(greeting, name) }}
func FilterFormat(_ State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	formatStr, ok := val.AsString()
	if !ok {
		// `&str`, so a non-string format string is an error rather than being
		// stringified (argtypes.rs:469-480).
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrInvalidOperation, "value is not a string")
	}
	if kwargs.Len() > 0 {
		// `Rest<Value>` collects positional arguments only; a keyword
		// argument arrives as a trailing Kwargs value and is not consumed.
		args = append(append([]value.Value(nil), args...), value.FromOrderedMap(kwargs))
	}
	formatted, err := pyformat.Format(pyformat.StylePrintf, formatStr, args, nil)
	if err != nil {
		return value.Undefined(), err
	}
	return value.FromString(formatted), nil
}

// FilterDefault provides a default value if the input is undefined.
//
// If the value is undefined, it returns the provided default value.
// Setting the optional second parameter to true will also treat empty/falsy
// values as undefined.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("default", FilterDefault)
//
// Template usage:
//
//	{{ my_variable|default("default value") }}
//	{{ ""|default("empty", true) }}
//	  -> "empty"
func FilterDefault(_ State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	a := NewArgs(args, kwargs)
	other, hasOther, err := a.OptValue()
	if err != nil {
		return value.Undefined(), err
	}
	lax, err := a.OptBool()
	if err != nil {
		return value.Undefined(), err
	}
	if err := a.Done(); err != nil {
		return value.Undefined(), err
	}

	fallback := func() value.Value {
		if hasOther {
			return other
		}
		return value.FromString("")
	}
	if val.IsUndefined() {
		return fallback(), nil
	}
	if lax && !val.IsTrue() {
		return fallback(), nil
	}
	return val, nil
}

// FilterSafe marks a value as safe for auto-escaping.
//
// When a value is marked as safe, it will not be automatically escaped
// when rendered in templates with auto-escaping enabled.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("safe", FilterSafe)
//
// Template usage:
//
//	{{ html_content|safe }}
//
// Warning: Only use this filter on values you trust to contain safe HTML.
// Using it on untrusted content can lead to security vulnerabilities.
func FilterSafe(_ State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	if err := noArgs(args, kwargs); err != nil {
		return value.Undefined(), err
	}
	if s, ok := val.AsString(); ok {
		return value.FromSafeString(s), nil
	}
	return value.FromSafeString(val.String()), nil
}

// FilterEscape escapes a string for safe HTML output.
//
// By default, this filter is also registered under the alias "e". If the value
// is already marked as safe, it is returned unchanged. Otherwise, it escapes
// HTML special characters.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("escape", FilterEscape)
//
// Template usage:
//
//	{{ user_input|escape }}
//	{{ "<script>alert('xss')</script>"|e }}
//	  -> "&lt;script&gt;alert('xss')&lt;/script&gt;"
func FilterEscape(_ State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	if err := noArgs(args, kwargs); err != nil {
		return value.Undefined(), err
	}
	if val.IsSafe() {
		return val, nil
	}
	if s, ok := val.AsString(); ok {
		return value.FromSafeString(EscapeHTML(s)), nil
	}
	return value.FromSafeString(EscapeHTML(val.String())), nil
}

// EscapeHTML escapes a string for safe use in HTML.
func EscapeHTML(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '&':
			b.WriteString("&amp;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&#x27;")
		case '/':
			b.WriteString("&#x2f;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// FilterString converts a value into a string if it's not one already.
//
// If the value is already a string (and marked as safe if applicable),
// that value is preserved. Otherwise, the value is converted to its
// string representation.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("string", FilterString)
//
// Template usage:
//
//	{{ 42|string }}
//	  -> "42"
func FilterString(_ State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	if err := noArgs(args, kwargs); err != nil {
		return value.Undefined(), err
	}
	if val.Kind() == value.KindString {
		return val, nil
	}
	return value.FromString(val.String()), nil
}

// FilterBool converts a value into a boolean.
//
// This filter evaluates the truthiness of a value according to MiniJinja's
// rules: non-zero numbers, non-empty strings, and non-empty collections
// are true.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("bool", FilterBool)
//
// Template usage:
//
//	{{ 42|bool }}
//	  -> true
//	{{ ""|bool }}
//	  -> false
func FilterBool(state State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	if err := noArgs(args, kwargs); err != nil {
		return value.Undefined(), err
	}
	behavior := undefinedBehavior(state)
	if val.IsUndefined() && behavior == value.UndefinedStrict && !val.IsSilentUndefined() {
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrUndefinedVar, "undefined value")
	}
	return value.FromBool(val.IsTrue()), nil
}

// FilterSplit splits a string into a list of substrings.
//
// If no split pattern is provided or it's none, the string is split on
// whitespace with multiple spaces removed. Otherwise, the string is split
// using the provided separator.
//
// The optional second parameter defines the maximum number of splits
// (following Python conventions where 1 means one split and two resulting items).
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("split", FilterSplit)
//
// Template usage:
//
//	{{ "hello world"|split }}
//	  -> ["hello", "world"]
//	{{ "a,b,c"|split(",") }}
//	  -> ["a", "b", "c"]
//	{{ "a,b,c,d"|split(",", 2) }}
//	  -> ["a", "b", "c,d"]
func FilterSplit(_ State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	// The engine's subject type here is `Arc<str>`, which — unlike the
	// `Cow<'_, str>` the casing filters take — requires a real string
	// (argtypes.rs:482-517).
	s, ok := val.AsString()
	if !ok {
		return value.Undefined(), invalidOp("value is not a string")
	}
	a := NewArgs(args, kwargs)
	sep, hasSep, err := a.OptStr()
	if err != nil {
		return value.Undefined(), err
	}
	maxSplits, hasMax, err := a.OptInt("i64")
	if err != nil {
		return value.Undefined(), err
	}
	if err := a.Done(); err != nil {
		return value.Undefined(), err
	}

	parts := Split(s, sep, hasSep, maxSplits, hasMax)
	result := make([]value.Value, len(parts))
	for i, p := range parts {
		result[i] = value.FromString(p)
	}
	// `make_object_iterable`, which renders as `<iterator>` rather than as a
	// list until something iterates it (filters.rs:475-484).
	return value.MakeIterable(func() iter.Seq[value.Value] {
		return func(yield func(value.Value) bool) {
			for _, item := range result {
				if !yield(item) {
					return
				}
			}
		}
	}), nil
}

// Split is the `split` filter's transform, shared with pycompat's str.split
// (filters.rs:475-484).
//
// Without a separator the string is split at runs of Unicode whitespace and
// empty pieces are dropped; with one it is split at every occurrence, so an
// empty separator yields the empty leading and trailing pieces Rust's
// str::split produces. A negative maxsplits means "no limit", and a
// non-negative one counts SPLITS, not pieces, as in Python.
func Split(s, sep string, hasSep bool, maxSplits int64, hasMax bool) []string {
	limit := -1
	if hasMax && maxSplits >= 0 {
		limit = int(maxSplits) + 1
	}

	switch {
	case !hasSep && limit < 0:
		return unicodecase.Fields(s)
	case !hasSep:
		return splitWhitespaceN(s, limit)
	case limit < 0:
		return splitAll(s, sep)
	default:
		return splitAllN(s, sep, limit)
	}
}

// splitAll is Rust's str::split for a string pattern, which — unlike Go's
// strings.Split — yields a leading and trailing empty piece for an empty
// separator: "ab".split("") is ["", "a", "b", ""].
func splitAll(s, sep string) []string {
	if sep != "" {
		return strings.Split(s, sep)
	}
	out := []string{""}
	for _, r := range s {
		out = append(out, string(r))
	}
	return append(out, "")
}

func splitAllN(s, sep string, n int) []string {
	if sep != "" {
		return strings.SplitN(s, sep, n)
	}
	all := splitAll(s, sep)
	if n >= len(all) {
		return all
	}
	out := append([]string(nil), all[:n-1]...)
	return append(out, strings.Join(all[n-1:], ""))
}

// splitWhitespaceN is minijinja's splitn_whitespace (utils.rs:398-434): at
// most n pieces, split at runs of Unicode whitespace, with the remainder —
// leading whitespace and all — as the last piece.
func splitWhitespaceN(s string, n int) []string {
	if n <= 0 {
		return nil
	}
	var out []string
	splits := 1
	skipWS := true
	splitStart := -1
	lastSplitEnd := 0

	for idx, r := range s {
		if splits >= n && !skipWS {
			continue
		}
		if unicodecase.IsSpace(r) {
			if splitStart >= 0 {
				out = append(out, s[splitStart:idx])
				splitStart = -1
				lastSplitEnd = idx
				splits++
				skipWS = true
			}
			continue
		}
		skipWS = false
		if splitStart < 0 {
			splitStart = idx
			lastSplitEnd = idx
		}
	}

	if rest := s[lastSplitEnd:]; rest != "" && splitStart >= 0 {
		out = append(out, rest)
	}
	return out
}

// FilterLines splits a string into lines.
//
// The newline character is removed in the process. This function supports
// both Windows (CRLF) and UNIX (LF) style newlines.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("lines", FilterLines)
//
// Template usage:
//
//	{{ "foo\nbar\nbaz"|lines }}
//	  -> ["foo", "bar", "baz"]
func FilterLines(_ State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	if err := noArgs(args, kwargs); err != nil {
		return value.Undefined(), err
	}
	s, ok := val.AsString()
	if !ok {
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrInvalidOperation, "value is not a string")
	}
	lines := Lines(s)
	result := make([]value.Value, len(lines))
	for i, line := range lines {
		result[i] = value.FromString(line)
	}
	return value.FromSlice(result), nil
}

// Lines is Rust's str::lines, which the `lines` filter and pycompat's
// str.splitlines are both defined in terms of: split at '\n', drop one
// trailing '\r' per line, and produce no empty final line for a trailing
// newline. A lone '\r' is NOT a line break.
func Lines(s string) []string {
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

// FilterLength returns the number of items in a collection or string.
//
// This filter works on sequences, maps, and strings. For strings, it returns
// the number of characters. This filter is also commonly available under the
// alias "count".
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("length", FilterLength)
//
// Template usage:
//
//	<p>{{ users|length }} users found</p>
//	{{ "hello"|length }}
//	  -> 5
func FilterLength(_ State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	if err := noArgs(args, kwargs); err != nil {
		return value.Undefined(), err
	}
	if l, ok := val.Len(); ok {
		return value.FromInt(int64(l)), nil
	}
	return value.Undefined(), mjerrors.NewError(mjerrors.ErrInvalidOperation,
		fmt.Sprintf("cannot calculate length of value of type %s", val.Kind()))
}

// FilterFirst returns the first item from an iterable.
//
// If the iterable is empty, undefined is returned.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("first", FilterFirst)
//
// Template usage:
//
//	<dl>
//	  <dt>primary email
//	  <dd>{{ user.email_addresses|first|default('no email') }}
//	</dl>
func FilterFirst(_ State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	if err := noArgs(args, kwargs); err != nil {
		return value.Undefined(), err
	}
	if s, ok := val.AsString(); ok {
		for _, r := range s {
			return value.FromString(string(r)), nil
		}
		return value.Undefined(), nil
	}
	// Unlike `list`, `first` reaches for the value as an object, so none and
	// undefined are errors rather than empty (filters.rs:686-697).
	//
	// It is `as_object().and_then(|x| x.try_iter())`, so the question is
	// whether the object ENUMERATES, not what it looks like: a map object with
	// no enumerable pairs takes the error arm rather than answering undefined.
	switch val.Kind() {
	case value.KindSeq, value.KindMap, value.KindIterable, value.KindPlain:
		if items := val.Iter(); items != nil {
			if len(items) > 0 {
				return items[0], nil
			}
			return value.Undefined(), nil
		}
	}
	return value.Undefined(), mjerrors.NewError(mjerrors.ErrInvalidOperation,
		"cannot get first item from value")
}

// FilterLast returns the last item from an iterable.
//
// If the iterable is empty, undefined is returned.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("last", FilterLast)
//
// Template usage:
//
//	<h2>Most Recent Update</h2>
//	{% with update = updates|last %}
//	  <dl>
//	    <dt>Location
//	    <dd>{{ update.location }}
//	    <dt>Status
//	    <dd>{{ update.status }}
//	  </dl>
//	{% endwith %}
func FilterLast(_ State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	if err := noArgs(args, kwargs); err != nil {
		return value.Undefined(), err
	}
	if s, ok := val.AsString(); ok {
		runes := []rune(s)
		if len(runes) == 0 {
			return value.Undefined(), nil
		}
		return value.FromString(string(runes[len(runes)-1])), nil
	}
	// `last` accepts only a string, a sequence or an iterable
	// (filters.rs:716-728) — unlike `first`, which reaches for the value as an
	// object and therefore also works on a mapping.
	switch val.Kind() {
	case value.KindSeq, value.KindIterable:
		items := val.Iter()
		if len(items) > 0 {
			return items[len(items)-1], nil
		}
		return value.Undefined(), nil
	default:
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrInvalidOperation,
			"cannot get last item from value")
	}
}

// FilterReverse reverses an iterable or string.
//
// For strings, this reverses the characters. For iterables, it reverses
// the order of items.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("reverse", FilterReverse)
//
// Template usage:
//
//	{% for user in users|reverse %}
//	  <li>{{ user.name }}
//	{% endfor %}
//	{{ "hello"|reverse }}
//	  -> "olleh"
func FilterReverse(_ State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	if err := noArgs(args, kwargs); err != nil {
		return value.Undefined(), err
	}
	if s, ok := val.AsString(); ok {
		runes := []rune(s)
		for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
			runes[i], runes[j] = runes[j], runes[i]
		}
		return value.FromString(string(runes)), nil
	}

	// none and undefined reverse to themselves; a value that cannot be
	// enumerated at all is an error (value/mod.rs:1400-1462).
	switch val.Kind() {
	case value.KindNone, value.KindUndefined:
		return val, nil
	case value.KindMap:
		// Reversing a mapping yields its keys in the mapping's own order, not
		// reversed. That is not a shortcut: the Rust engine enumerates a map with a
		// double-ended iterator and its `reverse` re-boxes that iterator without
		// calling .rev() on it, so `m|reverse` iterates forward. Reversing the
		// result then does reverse, because the result enumerates differently. Both
		// halves are pinned by the corpus (container/map-reverse-order and
		// container/map-reverse-twice), and `contract/filter-reverse-map` and
		// `-map-items` pin the same asymmetry from the argument-contract side.
		//
		// `Value::reverse` branches on the object's ENUMERATOR, and its very
		// first arm is `Enumerator::NonEnumerable => None` (value/mod.rs:1411).
		// A map object with no enumerable pairs is therefore the error arm, not
		// an empty iterator.
		items := val.Iter()
		if items == nil {
			return value.Undefined(), mjerrors.NewError(mjerrors.ErrInvalidOperation,
				fmt.Sprintf("cannot reverse values of type %s", val.Kind()))
		}
		return value.FromIterator(value.NewIterator("reversed", items)), nil
	case value.KindSeq, value.KindIterable, value.KindPlain:
		items := val.Iter()
		if items == nil && val.Kind() == value.KindPlain {
			return value.Undefined(), mjerrors.NewError(mjerrors.ErrInvalidOperation,
				fmt.Sprintf("cannot reverse values of type %s", val.Kind()))
		}
		result := make([]value.Value, len(items))
		for i, item := range items {
			result[len(items)-1-i] = item
		}
		return value.FromIterator(value.NewIterator("reversed", result)), nil
	default:
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrInvalidOperation,
			fmt.Sprintf("cannot reverse values of type %s", val.Kind()))
	}
}

// FilterSort sorts an iterable.
//
// The filter accepts several keyword arguments to control sorting behavior:
//
//   - reverse: set to true to sort in descending order
//   - case_sensitive: set to true for case-sensitive string sorting (default: false)
//   - attribute: can be set to an attribute name or dotted path to sort by that attribute
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("sort", FilterSort)
//
// Template usage:
//
//	{{ [3, 1, 2]|sort }}
//	  -> [1, 2, 3]
//	{{ users|sort(attribute="age") }}
//	{{ users|sort(attribute="age", reverse=true) }}
//	{{ cities|sort(attribute="name, state") }}
func FilterSort(state State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	a := NewArgs(args, kwargs).Kwargs()
	reverse, _, err := a.GetBool("reverse")
	if err != nil {
		return value.Undefined(), err
	}
	caseSensitive, _, err := a.GetBool("case_sensitive")
	if err != nil {
		return value.Undefined(), err
	}
	attrName, hasAttr, err := a.GetStr("attribute")
	if err != nil {
		return value.Undefined(), err
	}
	if err := a.Done(); err != nil {
		return value.Undefined(), err
	}

	// `Option<&str>`: the attribute branch is chosen by whether the keyword was
	// GIVEN, so `sort(attribute="")` is a real, empty path and not "no
	// attribute" (filters.rs:785).
	var sortKeys []string
	if hasAttr {
		sortKeys = splitAttributeKeys(attrName)
	}

	items, err := listArg(val)
	if err != nil {
		return value.Undefined(), err
	}

	result := make([]value.Value, len(items))
	copy(result, items)

	sort.SliceStable(result, func(i, j int) bool {
		a, b := result[i], result[j]

		// A comma-separated attribute is a COMPOSITE key, compared
		// element by element (filters.rs:785-819): `sort(attribute="age, name")`
		// orders by age and breaks ties by name, which one path lookup cannot
		// do.
		if len(sortKeys) == 1 {
			// The single-key fast path compares the two `get_path` RESULTS,
			// and a failed lookup on either side is a TIE, not undefined
			// (filters.rs:814-819): under a stable sort the pair keeps its
			// input order instead of being reordered against everything else
			// that also failed.
			av, aok := getDeepAttrOK(a, sortKeys[0])
			bv, bok := getDeepAttrOK(b, sortKeys[0])
			if !aok || !bok {
				return false
			}
			a, b = av, bv
		} else if len(sortKeys) > 1 {
			aParts := make([]value.Value, len(sortKeys))
			bParts := make([]value.Value, len(sortKeys))
			for i, key := range sortKeys {
				aParts[i] = getDeepAttr(a, key)
				bParts[i] = getDeepAttr(b, key)
			}
			a = value.FromSlice(aParts)
			b = value.FromSlice(bParts)
		}

		cmp := cmpHelper(a, b, caseSensitive)
		if reverse {
			cmp = -cmp
		}
		return cmp < 0
	})

	return value.FromSlice(result), nil
}

// cmpHelper is the engine's `cmp_helper` (filters.rs:284-307), the single
// comparator `sort` and `groupby` both use.
//
// Two things about it are load-bearing and were not reproduced by comparing
// lowercased strings:
//
//   - the case-insensitive path is UNICODE case folding, not lowercasing. It is
//     `UniCase::new(a).cmp(&UniCase::new(b))` under the `unicode` feature BAML
//     enables, so "İ" and "i̇" are EQUAL and "ß" ties with "ss".
//   - a tie is a tie. The comparator has no secondary key, so two strings that
//     fold alike keep their input order under the engine's stable sort — and
//     `groupby` puts them in one group. Breaking such a tie by case rank or by
//     raw bytes changed both the order of a sort and the number of groups.
//
// Anything that is not a pair of strings, and everything when case_sensitive is
// set, falls to the value model's own total order.
func cmpHelper(a, b value.Value, caseSensitive bool) int {
	if !caseSensitive {
		if s1, ok1 := a.AsString(); ok1 {
			if s2, ok2 := b.AsString(); ok2 {
				return unicodecase.FoldCompare(s1, s2)
			}
		}
	}
	if cmp, ok := a.Compare(b); ok {
		return cmp
	}
	return 0
}

// splitAttributeKeys splits `sort`'s attribute into its comma-separated parts
// (filters.rs:786-796). Only `sort` splits: every other consumer of an
// attribute passes the whole string to [getDeepAttr] as one path.
//
// Two details of the engine's filter are load-bearing and are reproduced here
// rather than tidied up:
//
//   - the EMPTINESS test is on the raw part and the kept value is the TRIMMED
//     one, so `" "` survives as `""` while `""` is dropped;
//   - when every part is dropped, `sort` falls back to the literal attribute
//     (filters.rs:813). `sort(attribute=",")` therefore looks up the key ","
//     rather than composing two empty ones.
func splitAttributeKeys(attr string) []string {
	parts := strings.Split(attr, ",")
	keys := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		keys = append(keys, strings.TrimSpace(part))
	}
	if len(keys) == 0 {
		return []string{attr}
	}
	return keys
}

// getDeepAttr walks the engine's attribute PATH, `Value::get_path`
// (value/mod.rs:1654-1664): the path splits on `.`, and a component is an
// index only when it parses COMPLETELY as a `usize`.
//
// The result also carries whether the walk succeeded, because `get_path`
// returns a Result and `sort`'s single-key comparator treats a failure as a
// TIE rather than as undefined (filters.rs:814-819).
func getDeepAttrOK(v value.Value, path string) (value.Value, bool) {
	for _, part := range strings.Split(path, ".") {
		// `get_attr`/`get_item` on undefined is the engine's one error here
		// (value/mod.rs:1286, 1337); every other miss is a plain undefined.
		if v.IsUndefined() {
			return v, false
		}
		if idx, ok := parseUsize(part); ok {
			v = v.GetItem(value.FromUint64(idx))
		} else {
			v = v.GetAttr(part)
		}
	}
	return v, true
}

// getDeepAttr is [getDeepAttrOK] for the consumers that use
// `get_path_or_default`, where a failed walk and an undefined result are the
// same answer (value/mod.rs:1667-1673).
func getDeepAttr(v value.Value, path string) value.Value {
	val, ok := getDeepAttrOK(v, path)
	if !ok {
		return value.Undefined()
	}
	return val
}

// parseUsize is `str::parse::<usize>()`: the WHOLE component must be a
// 64-bit unsigned integer. A sign other than `+`, a digit separator, or any
// trailing text makes the component an attribute NAME instead, which is why
// `sort(attribute="-1")` reads the key "-1" and does not index from the end.
func parseUsize(s string) (uint64, bool) {
	s = strings.TrimPrefix(s, "+")
	if s == "" {
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// FilterJoin concatenates items from an iterable into a string.
//
// The optional first parameter is the separator string to use between items.
// If not provided, items are concatenated directly without a separator.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("join", FilterJoin)
//
// Template usage:
//
//	{{ ["a", "b", "c"]|join(", ") }}
//	  -> "a, b, c"
//	{{ items|join }}
func FilterJoin(_ State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	a := NewArgs(args, kwargs)
	sep, _, err := a.OptCoerceStr()
	if err != nil {
		return value.Undefined(), err
	}
	if err := a.Done(); err != nil {
		return value.Undefined(), err
	}
	// none and undefined join to the empty string before anything else is
	// considered (filters.rs:429-432).
	if val.IsNone() || val.IsUndefined() {
		return value.FromString(""), nil
	}
	items, err := tryIter(val, fmt.Sprintf("cannot join value of type %s", val.Kind()))
	if err != nil {
		return value.Undefined(), err
	}

	parts := make([]string, len(items))
	for i, item := range items {
		parts[i] = item.String()
	}
	return value.FromString(strings.Join(parts, sep)), nil
}

// FilterList converts a value into a list.
//
// If the value is already a list, it's returned unchanged. For maps, this
// returns a list of keys. For strings, this returns the characters. If the
// value is undefined, an empty list is returned.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("list", FilterList)
//
// Template usage:
//
//	{{ "abc"|list }}
//	  -> ["a", "b", "c"]
//	{{ range(5)|list }}
//	  -> [0, 1, 2, 3, 4]
func FilterList(state State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	if err := NewArgs(args, kwargs).Done(); err != nil {
		return value.Undefined(), err
	}
	if val.IsUndefined() {
		behavior := undefinedBehavior(state)
		if (behavior == value.UndefinedStrict || behavior == value.UndefinedSemiStrict) && !val.IsSilentUndefined() {
			return value.Undefined(), mjerrors.NewError(mjerrors.ErrInvalidOperation, "cannot convert value to list")
		}
		return value.FromSlice(nil), nil
	}

	items, err := tryIter(val, "cannot convert value to list")
	if err != nil {
		return value.Undefined(), err
	}
	return value.FromSlice(items), nil
}

// FilterUnique returns unique items from an iterable.
//
// The unique items are yielded in the same order as their first occurrence.
// The filter will not detect duplicate objects or arrays, only primitives.
//
// Keyword arguments:
//   - case_sensitive: set to true for case-sensitive comparison (default: false)
//   - attribute: operate on an attribute instead of the value itself
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("unique", FilterUnique)
//
// Template usage:
//
//	{{ ["a", "b", "a", "c"]|unique }}
//	  -> ["a", "b", "c"]
//	{{ users|unique(attribute="city") }}
func FilterUnique(_ State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	a := NewArgs(args, kwargs).Kwargs()
	caseSensitive, _, err := a.GetBool("case_sensitive")
	if err != nil {
		return value.Undefined(), err
	}
	attrName, hasAttr, err := a.GetStr("attribute")
	if err != nil {
		return value.Undefined(), err
	}
	if err := a.Done(); err != nil {
		return value.Undefined(), err
	}

	items, err := iterArg(val)
	if err != nil {
		return value.Undefined(), err
	}

	var seen valueSet
	var result []value.Value
	for _, item := range items {
		valueToCompare := item
		// `Option<&str>`: an attribute that was given is a path even when it
		// is empty (filters.rs:1503, 1512-1516).
		if hasAttr {
			valueToCompare = getDeepAttr(item, attrName)
		}

		// Only an ACTUAL string is memoized case-folded. Everything else is
		// remembered as the value itself (filters.rs:1517-1523), so two
		// objects that merely render alike are two values.
		//
		// The lowercasing is Rust's `to_lowercase`, FULL case mapping: "İ" and
		// "i̇" fold together where Go's simple ToLower keeps them apart.
		memorized := valueToCompare
		if !caseSensitive {
			if s, ok := valueToCompare.AsString(); ok {
				memorized = value.FromString(unicodecase.ToLower(s))
			}
		}
		if seen.insert(memorized) {
			result = append(result, item)
		}
	}
	return value.FromSlice(result), nil
}

// valueSet is `BTreeSet<Value>`: membership is decided by the value model's
// own total ORDER, not by a rendered key.
//
// That is what makes `1` and `1.0` one entry — they coerce and compare equal —
// while `true` stays its own, and what routes two host objects through the
// generic comparison hook instead of collapsing them onto a shared display
// string. It is a sorted slice rather than a tree because the sets a template
// builds are small and the comparator is the expensive part either way.
type valueSet struct {
	sorted []value.Value
}

// insert adds a value and reports whether it was new.
func (s *valueSet) insert(v value.Value) bool {
	lo, hi := 0, len(s.sorted)
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		// A pair the engine has no rule for at all is where its `Ord` is
		// `unreachable!()`; treating it as equal is the only answer that keeps
		// this a total order.
		cmp, _ := s.sorted[mid].Compare(v)
		if cmp < 0 {
			lo = mid + 1
		} else if cmp > 0 {
			hi = mid
		} else {
			return false
		}
	}
	s.sorted = append(s.sorted, value.Value{})
	copy(s.sorted[lo+1:], s.sorted[lo:])
	s.sorted[lo] = v
	return true
}

// FilterMin returns the smallest item from an iterable.
//
// If the iterable is empty, undefined is returned.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("min", FilterMin)
//
// Template usage:
//
//	{{ [1, 2, 3, 4]|min }}
//	  -> 1
func FilterMin(_ State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	if err := NewArgs(args, kwargs).Done(); err != nil {
		return value.Undefined(), err
	}
	items, err := listArg(val)
	if err != nil {
		return value.Undefined(), err
	}
	if len(items) == 0 {
		return value.Undefined(), nil
	}

	minVal := items[0]
	for _, item := range items[1:] {
		if cmp, ok := item.Compare(minVal); ok && cmp < 0 {
			minVal = item
		}
	}
	return minVal, nil
}

// FilterMax returns the largest item from an iterable.
//
// If the iterable is empty, undefined is returned.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("max", FilterMax)
//
// Template usage:
//
//	{{ [1, 2, 3, 4]|max }}
//	  -> 4
func FilterMax(_ State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	if err := NewArgs(args, kwargs).Done(); err != nil {
		return value.Undefined(), err
	}
	items, err := listArg(val)
	if err != nil {
		return value.Undefined(), err
	}
	if len(items) == 0 {
		return value.Undefined(), nil
	}

	maxVal := items[0]
	for _, item := range items[1:] {
		if cmp, ok := item.Compare(maxVal); ok && cmp > 0 {
			maxVal = item
		}
	}
	return maxVal, nil
}

// FilterSum sums up all numeric values in an iterable.
//
// The optional first parameter provides a start value (default is 0).
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("sum", FilterSum)
//
// Template usage:
//
//	{{ [1, 2, 3]|sum }}
//	  -> 6
//	{{ values|sum(100) }}
//	  -> sum of values + 100
//
// The engine's `sum` takes no arguments and refuses a non-number
// (filters.rs:616-633). Jinja2's `start` argument is not part of it.
//
// BAML replaces this filter in its own environment with one that has an
// asymmetric int/float rule; that belongs to the deferred BAML profile, not
// here.
func FilterSum(_ State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	if len(args) > 0 || kwargs.Len() > 0 {
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrTooManyArguments, "too many arguments")
	}
	items, err := iterArg(val)
	if err != nil {
		return value.Undefined(), err
	}

	result := value.FromInt(0)
	for _, item := range items {
		if item.IsUndefined() {
			continue
		}
		if item.Kind() != value.KindNumber {
			return value.Undefined(), mjerrors.NewError(mjerrors.ErrInvalidOperation,
				fmt.Sprintf("can only sum numbers, got %s", item.Kind()))
		}
		result, err = result.Add(item)
		if err != nil {
			return value.Undefined(), err
		}
	}
	return result, nil
}

// FilterBatch batches items into groups of a given size.
//
// This filter works like FilterSlice but in the other direction. It returns
// a list of lists with the given number of items. If you provide a second
// parameter, it's used to fill up missing items in the last batch.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("batch", FilterBatch)
//
// Template usage:
//
//	<table>
//	{% for row in items|batch(3, " ") %}
//	  <tr>
//	  {% for column in row %}
//	    <td>{{ column }}</td>
//	  {% endfor %}
//	  </tr>
//	{% endfor %}
//	</table>
func FilterBatch(_ State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	a := NewArgs(args, kwargs)
	rawCount, err := countArg(a)
	if err != nil {
		return value.Undefined(), err
	}
	fill, hasFill, err := a.OptValue()
	if err != nil {
		return value.Undefined(), err
	}
	if err := a.Done(); err != nil {
		return value.Undefined(), err
	}
	lineCount, err := usableCount(rawCount)
	if err != nil {
		return value.Undefined(), err
	}
	items, err := iterArg(val)
	if err != nil {
		return value.Undefined(), err
	}

	fillWith := value.Undefined()
	if hasFill {
		fillWith = fill
	}

	var result []value.Value
	for i := 0; i < len(items); i += lineCount {
		end := i + lineCount
		if end > len(items) {
			end = len(items)
		}
		batch := make([]value.Value, end-i)
		copy(batch, items[i:end])

		// Fill the last batch if needed
		if !fillWith.IsUndefined() && len(batch) < lineCount {
			for len(batch) < lineCount {
				batch = append(batch, fillWith)
			}
		}
		result = append(result, value.FromSlice(batch))
	}
	return value.FromSlice(result), nil
}

// FilterSlice slices an iterable into a given number of columns.
//
// This filter works like FilterBatch but slices into columns instead of rows.
// If you pass a second argument, it's used to fill missing values on the
// last iteration.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("slice", FilterSlice)
//
// Template usage:
//
//	<div class="columnwrapper">
//	{% for column in items|slice(3) %}
//	  <ul class="column-{{ loop.index }}">
//	  {% for item in column %}
//	    <li>{{ item }}</li>
//	  {% endfor %}
//	  </ul>
//	{% endfor %}
//	</div>
//
// countArg is the `count: usize` argument `batch` and `slice` take. It only
// CONVERTS, at the width the parameter really declares, so a count in the upper
// half of u64 is accepted here exactly as it is by the engine's ArgType.
//
// Everything the value then has to satisfy — non-zero (filters.rs:945-953) and
// allocatable — belongs to the filter BODY, which the engine only reaches after
// `from_args` has bound every parameter and rejected a surplus one. Checking
// them here reported `[]|batch(<huge>, 1, 2)` as an invalid operation where the
// engine reports too many arguments. See [usableCount].
func countArg(a *Args) (uint64, error) {
	return a.Usize()
}

// usableCount is the body's half of the `count` contract: run it after Done.
func usableCount(n uint64) (int, error) {
	if n == 0 {
		return 0, invalidOp("count cannot be 0")
	}
	return allocSize(n, "count")
}

func FilterSlice(_ State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	a := NewArgs(args, kwargs)
	rawCount, err := countArg(a)
	if err != nil {
		return value.Undefined(), err
	}
	fill, hasFill, err := a.OptValue()
	if err != nil {
		return value.Undefined(), err
	}
	if err := a.Done(); err != nil {
		return value.Undefined(), err
	}
	sliceCount, err := usableCount(rawCount)
	if err != nil {
		return value.Undefined(), err
	}
	items, err := iterArg(val)
	if err != nil {
		return value.Undefined(), err
	}

	// filters.rs:888-925: `items_per_slice` is the floor, the first
	// `slices_with_extra` slices take one more, and the filler is appended to
	// every slice that did NOT take an extra item.
	length := len(items)
	baseSize := length / sliceCount
	remainder := length % sliceCount

	var result []value.Value
	offset := 0
	for i := 0; i < sliceCount; i++ {
		size := baseSize
		if i < remainder {
			size++
		}

		slice := make([]value.Value, size)
		copy(slice, items[offset:offset+size])

		// The filler goes to every slice that did not take an extra item,
		// even when that makes it longer than the others.
		if hasFill && i >= remainder {
			slice = append(slice, fill)
		}

		result = append(result, value.FromSlice(slice))
		offset += size
	}
	return value.FromSlice(result), nil
}

// FilterMap applies a filter to a sequence or looks up an attribute.
//
// This is useful when dealing with lists of objects where you're only
// interested in a specific value. You can either map an attribute or apply
// a filter to each item.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("map", FilterMap)
//
// Template usage (attribute mapping):
//
//	{{ users|map(attribute="username")|join(", ") }}
//	{{ users|map(attribute="address.city", default="Unknown")|join }}
//
// Template usage (filter mapping):
//
//	{{ titles|map("lower")|join(", ") }}
//
// Keyword arguments:
//   - attribute: name or dotted path of attribute to extract
//   - default: value to use when attribute is missing
func FilterMap(state State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	// `map(state, value, args: Rest<Value>)`, which is then split back into
	// positional arguments and keyword arguments (filters.rs:1293-1353). The
	// attribute form takes no positional arguments at all, and the filter form
	// requires a string name.
	a := NewArgs(args, kwargs).Kwargs()
	rest := a.Rest()
	attrValue, hasAttr := a.Get("attribute")
	defaultVal, hasDefault := a.Get("default")
	if !hasDefault {
		defaultVal = value.Undefined()
	}

	var attrName string
	var filterName string
	attributeForm := hasAttr && !attrValue.IsUndefined() && !attrValue.IsNone()
	if attributeForm {
		// The attribute form takes no positional arguments at all.
		if len(rest) > 0 {
			return value.Undefined(), tooMany()
		}
		if s, ok := attrValue.AsString(); ok {
			attrName = s
		}
	} else {
		attrValue = value.Undefined()
		if len(rest) == 0 {
			return value.Undefined(), invalidOp("filter name is required")
		}
		name, ok := rest[0].AsString()
		if !ok {
			return value.Undefined(), invalidOp("filter name must be a string")
		}
		filterName = name
	}
	// Only the attribute branch asserts that every keyword was used
	// (filters.rs:1329); the filter branch never looks at them again, so an
	// unknown keyword is silently accepted there. Reproduced rather than
	// tidied up: the corpus row `contract/filter-map-keyword` pins it.
	if attributeForm {
		if err := a.Done(); err != nil {
			return value.Undefined(), err
		}
	}

	items, err := iterArg(val)
	if err != nil {
		return value.Undefined(), err
	}

	var result []value.Value
	for _, item := range items {
		var mapped value.Value
		if !attrValue.IsUndefined() {
			// Attribute mapping with dot notation support
			if attrName != "" {
				mapped = getDeepAttr(item, attrName)
			} else {
				mapped = item.GetItem(attrValue)
			}
			if mapped.IsUndefined() && !defaultVal.IsUndefined() {
				mapped = defaultVal
			}
		} else if filterName != "" {
			// Filter mapping
			filterFn, ok := state.GetFilter(filterName)
			if !ok {
				return value.Undefined(), mjerrors.NewError(mjerrors.ErrUnknownFilter, "unknown filter")
			}
			var err error
			mapped, err = filterFn(state, item, rest[1:], nil)
			if err != nil {
				return value.Undefined(), err
			}
		} else {
			return value.Undefined(), invalidOp("filter name is required")
		}
		result = append(result, mapped)
	}
	return value.FromSlice(result), nil
}

func normalizeTestName(name string) string {
	switch name {
	case "==":
		return "eq"
	case "!=":
		return "ne"
	case ">":
		return "gt"
	case ">=":
		return "ge"
	case "<":
		return "lt"
	case "<=":
		return "le"
	default:
		return name
	}
}

// FilterSelect filters a sequence by applying a test.
//
// This creates a new sequence containing only values that pass the test.
// If no test is specified, items are evaluated for truthiness.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("select", FilterSelect)
//
// Template usage:
//
//	{{ [1, 2, 3, 4]|select("odd") }}
//	  -> [1, 3]
//	{{ [false, null, 42]|select }}
//	  -> [42]
func FilterSelect(state State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	// `(state, value, test_name: Option<Cow<str>>, args: Rest<Value>)`: a
	// keyword argument lands in test_name, where a map is not a string.
	a := NewArgs(args, kwargs)
	name, hasName, err := a.OptCoerceStr()
	if err != nil {
		return value.Undefined(), err
	}
	testArgs := a.Rest()
	testName := ""
	if hasName {
		testName = normalizeTestName(name)
	}

	items, err := iterArg(val)
	if err != nil {
		return value.Undefined(), err
	}

	var result []value.Value
	for _, item := range items {
		var keep bool
		if testName != "" {
			testFn, ok := state.GetTest(testName)
			if !ok {
				return value.Undefined(), mjerrors.NewError(mjerrors.ErrUnknownTest, "unknown test")
			}
			var err error
			keep, err = testFn(state, item, testArgs)
			if err != nil {
				return value.Undefined(), err
			}
		} else {
			keep = item.IsTrue()
		}
		if keep {
			result = append(result, item)
		}
	}
	return value.FromSlice(result), nil
}

// FilterReject filters a sequence by rejecting values that pass a test.
//
// This is the inverse of FilterSelect - it creates a new sequence containing
// only values that fail the test.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("reject", FilterReject)
//
// Template usage:
//
//	{{ [1, 2, 3, 4]|reject("odd") }}
//	  -> [2, 4]
func FilterReject(state State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	// `(state, value, test_name: Option<Cow<str>>, args: Rest<Value>)`: a
	// keyword argument lands in test_name, where a map is not a string.
	a := NewArgs(args, kwargs)
	name, hasName, err := a.OptCoerceStr()
	if err != nil {
		return value.Undefined(), err
	}
	testArgs := a.Rest()
	testName := ""
	if hasName {
		testName = normalizeTestName(name)
	}

	items, err := iterArg(val)
	if err != nil {
		return value.Undefined(), err
	}

	var result []value.Value
	for _, item := range items {
		var reject bool
		if testName != "" {
			testFn, ok := state.GetTest(testName)
			if !ok {
				return value.Undefined(), mjerrors.NewError(mjerrors.ErrUnknownTest, "unknown test")
			}
			var err error
			reject, err = testFn(state, item, testArgs)
			if err != nil {
				return value.Undefined(), err
			}
		} else {
			reject = item.IsTrue()
		}
		if !reject {
			result = append(result, item)
		}
	}
	return value.FromSlice(result), nil
}

// FilterSelectAttr filters a sequence by testing an attribute.
//
// This is like FilterSelect but tests an attribute of each object instead
// of the object itself.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("selectattr", FilterSelectAttr)
//
// Template usage:
//
//	{{ users|selectattr("is_active") }}
//	  -> all users where x.is_active is true
//	{{ users|selectattr("id", "even") }}
//	  -> users with even IDs
func FilterSelectAttr(state State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	a := NewArgs(args, kwargs)
	attrName, err := a.CoerceStr()
	if err != nil {
		return value.Undefined(), err
	}
	name, hasName, err := a.OptCoerceStr()
	if err != nil {
		return value.Undefined(), err
	}
	testArgs := a.Rest()
	testName := ""
	if hasName {
		testName = normalizeTestName(name)
	}

	items, err := iterArg(val)
	if err != nil {
		return value.Undefined(), err
	}

	var result []value.Value
	for _, item := range items {
		attr := getDeepAttr(item, attrName)
		var keep bool
		if testName != "" {
			testFn, ok := state.GetTest(testName)
			if !ok {
				return value.Undefined(), mjerrors.NewError(mjerrors.ErrUnknownTest, "unknown test")
			}
			var err error
			keep, err = testFn(state, attr, testArgs)
			if err != nil {
				return value.Undefined(), err
			}
		} else {
			keep = attr.IsTrue()
		}
		if keep {
			result = append(result, item)
		}
	}
	return value.FromSlice(result), nil
}

// FilterRejectAttr filters a sequence by rejecting items where an attribute passes a test.
//
// This is like FilterReject but tests an attribute of each object instead
// of the object itself.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("rejectattr", FilterRejectAttr)
//
// Template usage:
//
//	{{ users|rejectattr("is_active") }}
//	  -> all users where x.is_active is false
//	{{ users|rejectattr("id", "even") }}
//	  -> users with odd IDs
func FilterRejectAttr(state State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	a := NewArgs(args, kwargs)
	attrName, err := a.CoerceStr()
	if err != nil {
		return value.Undefined(), err
	}
	name, hasName, err := a.OptCoerceStr()
	if err != nil {
		return value.Undefined(), err
	}
	testArgs := a.Rest()
	testName := ""
	if hasName {
		testName = normalizeTestName(name)
	}

	items, err := iterArg(val)
	if err != nil {
		return value.Undefined(), err
	}

	var result []value.Value
	for _, item := range items {
		attr := getDeepAttr(item, attrName)
		var reject bool
		if testName != "" {
			testFn, ok := state.GetTest(testName)
			if !ok {
				return value.Undefined(), mjerrors.NewError(mjerrors.ErrUnknownTest, "unknown test")
			}
			var err error
			reject, err = testFn(state, attr, testArgs)
			if err != nil {
				return value.Undefined(), err
			}
		} else {
			reject = attr.IsTrue()
		}
		if !reject {
			result = append(result, item)
		}
	}
	return value.FromSlice(result), nil
}

// FilterGroupBy groups a sequence of objects by a common attribute.
//
// The attribute can use dot notation for nested access. Items are automatically
// sorted first. Each group is returned as a tuple/object with "grouper" and "list"
// attributes.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("groupby", FilterGroupBy)
//
// Template usage:
//
//	<ul>{% for city, items in users|groupby("city") %}
//	  <li>{{ city }}
//	    <ul>{% for user in items %}
//	      <li>{{ user.name }}
//	    {% endfor %}</ul>
//	  </li>
//	{% endfor %}</ul>
//
// Keyword arguments:
//   - attribute: name or dotted path of attribute to group by
//   - default: value to use when attribute is missing
//   - case_sensitive: if true, sort in a case-sensitive manner (default: false)
func FilterGroupBy(_ State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	// `groupby(value, attribute: Option<&str>, kwargs)` (filters.rs:1404-1417).
	// The Kwargs parameter is extracted from the end BEFORE the positional
	// ones are filled (argtypes.rs:199-210), so `groupby(attribute="city")`
	// leaves the positional attribute slot empty rather than putting a map in
	// it.
	ga := NewArgs(args, kwargs).Kwargs()
	attrName, hasAttr, err := ga.OptStr()
	if err != nil {
		return value.Undefined(), err
	}
	defaultVal, hasDefault := ga.Get("default")
	if !hasDefault {
		defaultVal = value.Undefined()
	}
	caseSensitive, _, err := ga.GetBool("case_sensitive")
	if err != nil {
		return value.Undefined(), err
	}
	if !hasAttr {
		// The attribute may come as a keyword instead, and it is required
		// either way.
		attrName, hasAttr, err = ga.GetStr("attribute")
		if err != nil {
			return value.Undefined(), err
		}
		if !hasAttr {
			return value.Undefined(), missing()
		}
	}
	if err := ga.Done(); err != nil {
		return value.Undefined(), err
	}

	items, err := iterArg(val)
	if err != nil {
		return value.Undefined(), err
	}

	// Sort items by group key
	sorted := make([]value.Value, len(items))
	copy(sorted, items)

	// The SAME comparator groups and sorts (filters.rs:1412-1416 and 1455), so
	// two keys that tie under it end up adjacent AND in one group. Sorting with
	// a stricter comparator than the grouping test used to reorder a
	// case-insensitive tie and then split it.
	sort.SliceStable(sorted, func(i, j int) bool {
		left := groupByValue(sorted[i], attrName, defaultVal)
		right := groupByValue(sorted[j], attrName, defaultVal)
		return cmpHelper(left, right, caseSensitive) < 0
	})

	// Group items
	var result []value.Value
	var currentGrouper value.Value
	var currentList []value.Value
	hasGroup := false

	for _, item := range sorted {
		groupValue := groupByValue(item, attrName, defaultVal)
		if !hasGroup {
			currentGrouper = groupValue
			currentList = []value.Value{item}
			hasGroup = true
			continue
		}

		if cmpHelper(currentGrouper, groupValue, caseSensitive) != 0 {
			result = append(result, value.FromObject(&groupObject{
				grouper: currentGrouper,
				list:    currentList,
			}))
			currentGrouper = groupValue
			currentList = []value.Value{item}
			continue
		}

		currentGrouper = groupValue
		currentList = append(currentList, item)
	}

	if hasGroup {
		result = append(result, value.FromObject(&groupObject{
			grouper: currentGrouper,
			list:    currentList,
		}))
	}

	return value.FromSlice(result), nil
}

func groupByValue(item value.Value, attrName string, defaultVal value.Value) value.Value {
	grouper := getDeepAttr(item, attrName)
	if grouper.IsUndefined() {
		grouper = defaultVal
	}
	return grouper
}

// groupObject is groupby's group, the engine's `GroupTuple`
// (filters.rs:1419-1446). Its two observable KINDS are not the same:
//
//   - the tuple itself is a SEQUENCE of exactly two elements — `repr()` is
//     ObjectRepr::Seq and `enumerate()` is Enumerator::Seq(2) — so
//     `group is sequence` is true, `group|length` is 2, and `group[-1]` counts
//     back from the end.
//   - its `.list` projection is an ITERABLE whose length is known, built with
//     `Value::make_object_iterable`. So `group.list is sequence` is FALSE while
//     `group.list is iterable` is true and `group.list|length` answers.
//
// Materializing both as plain lists made the first false and the second true,
// which is directly observable through the standard `is sequence` test.
type groupObject struct {
	grouper value.Value
	list    []value.Value
}

var (
	_ value.Object         = (*groupObject)(nil)
	_ value.ObjectWithRepr = (*groupObject)(nil)
	_ value.SeqObject      = (*groupObject)(nil)
)

// GetAttr is the string half of `get_value`: the tuple answers to `grouper` and
// `list` as well as to 0 and 1.
func (g *groupObject) GetAttr(name string) value.Value {
	switch name {
	case "grouper":
		return g.grouper
	case "list":
		return g.listValue()
	}
	return value.Undefined()
}

func (g *groupObject) ObjectRepr() value.ObjectRepr { return value.ObjectReprSeq }

func (g *groupObject) SeqLen() int { return 2 }

func (g *groupObject) SeqItem(index int) value.Value {
	switch index {
	case 0:
		return g.grouper
	case 1:
		return g.listValue()
	}
	return value.Undefined()
}

func (g *groupObject) listValue() value.Value { return value.MakeSizedIterable(g.list) }

func (g *groupObject) String() string {
	return value.FromSlice([]value.Value{g.grouper, g.listValue()}).String()
}

// FilterChain chains multiple iterables into a single iterable.
//
// If all objects are maps, the result acts like a merged map (with later
// values overriding earlier ones for duplicate keys). If all objects are
// sequences, the result acts like an appended list. Otherwise, it creates
// a chained iterator.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("chain", FilterChain)
//
// Template usage:
//
//	{{ list1|chain(list2, list3)|length }}
//	{% for user in shard0|chain(shard1, shard2) %}
//	  {{ user.name }}
//	{% endfor %}
func FilterChain(_ State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	// `chain(value, others: Rest<Value>)` (filters.rs:1556-1583): a keyword
	// argument is one more operand, not something to ignore.
	others := NewArgs(args, kwargs).Rest()
	allValues := append([]value.Value{val}, others...)

	allMaps := true
	allSeq := true
	for _, v := range allValues {
		if v.Kind() != value.KindMap {
			allMaps = false
		}
		if v.Kind() != value.KindSeq {
			allSeq = false
		}
	}

	// All mappings merge into one mapping, all sequences into one sequence.
	//
	// The mapping is `MergeDict`, which does not build a merged map at all: it
	// searches its sources NEWEST FIRST and skips an undefined answer
	// (value/merge_object.rs:21-31). Eagerly overwriting instead lost an
	// earlier DEFINED value to a later undefined one, so
	// `dict(a=1)|chain(dict(a=missing))` answered undefined where the engine
	// answers 1.
	if allMaps {
		return value.NewMergedMap(allValues...), nil
	}
	if allSeq {
		var items []value.Value
		for _, v := range allValues {
			items = append(items, v.Iter()...)
		}
		return value.FromSlice(items), nil
	}

	// Anything else is a lazy chained iterable, and an operand that cannot be
	// iterated is yielded as an invalid value rather than dropped: the engine
	// keeps the failure visible in the stream (filters.rs:1574-1581), so
	// `{{ 42|chain([1])|list }}` is
	// `[<invalid value: invalid operation: number is not iterable>, 1]`.
	// Being an iterable and not a sequence, it has no length and no index,
	// which is why `42|chain([1])|length` is an invalid operation.
	var items []value.Value
	for _, v := range allValues {
		operand, err := tryIter(v, fmt.Sprintf("%s is not iterable", v.Kind()))
		if err != nil {
			items = append(items, value.Invalid(err))
			continue
		}
		items = append(items, operand...)
	}
	return value.FromObject(&chainObject{items: items}), nil
}

// chainObject is the lazy chained iterable `|chain` produces when its operands
// are not all mappings or all sequences. It is an ITERABLE, not a sequence:
// the engine's has no length and no index either.
type chainObject struct {
	items []value.Value
}

var (
	_ value.Object         = (*chainObject)(nil)
	_ value.IterableObject = (*chainObject)(nil)
	_ value.ObjectWithRepr = (*chainObject)(nil)
)

func (c *chainObject) GetAttr(name string) value.Value {
	return value.Undefined()
}

// String renders the way the engine's lazy iterable does.
func (c *chainObject) String() string { return "<iterator>" }

func (c *chainObject) ObjectRepr() value.ObjectRepr { return value.ObjectReprIterable }

func (c *chainObject) Iterate() iter.Seq[value.Value] {
	return func(yield func(value.Value) bool) {
		for _, item := range c.items {
			if !yield(item) {
				return
			}
		}
	}
}

// FilterZip zips multiple iterables into tuples.
//
// This works like Python's zip function. It takes one or more iterables and
// returns an iterable of tuples where each tuple contains one element from
// each input. Iteration stops when the shortest iterable is exhausted.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("zip", FilterZip)
//
// Template usage:
//
//	{{ [1, 2, 3]|zip(["a", "b", "c"]) }}
//	  -> [(1, "a"), (2, "b"), (3, "c")]
//	{{ [1, 2]|zip(["a", "b", "c"], ["x", "y", "z"]) }}
//	  -> [(1, "a", "x"), (2, "b", "y")]
func FilterZip(_ State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	// `zip(value, others: Rest<Value>)`: every operand is iterated, so a
	// non-iterable anywhere is an invalid operation rather than an empty
	// result.
	operands := append([]value.Value{val}, NewArgs(args, kwargs).Rest()...)

	// The validation pass walks the operands in order and STOPS at the first
	// one whose length is not known (filters.rs:1605-1627): the `break` is the
	// engine's, so a non-iterable operand after an unsized one is never
	// reported. The minimum of the known lengths is the zip's own length, and
	// one unsized operand takes it away entirely.
	knownLen := -1
	for _, operand := range operands {
		if _, err := tryIter(operand,
			fmt.Sprintf("zip filter argument must be iterable, got %s", operand.Kind())); err != nil {
			return value.Undefined(), err
		}
		n, ok := operand.Len()
		if !ok {
			knownLen = -1
			break
		}
		if knownLen < 0 || n < knownLen {
			knownLen = n
		}
	}

	// `make_object_iterable` (filters.rs:1629-1659), so the result is an
	// ITERABLE and not a sequence, whatever the operands were. An operand that
	// cannot be iterated at this point empties the whole stream rather than
	// failing it, which is what `collect::<Option<Vec<_>>>().unwrap_or_default()`
	// does.
	zip := func() []value.Value {
		seqs := make([][]value.Value, 0, len(operands))
		for _, operand := range operands {
			items, err := tryIter(operand, "not iterable")
			if err != nil {
				return nil
			}
			seqs = append(seqs, items)
		}
		minLen := math.MaxInt
		for _, seq := range seqs {
			if len(seq) < minLen {
				minLen = len(seq)
			}
		}
		if minLen <= 0 || minLen == math.MaxInt {
			return nil
		}
		result := make([]value.Value, minLen)
		for i := range result {
			tuple := make([]value.Value, len(seqs))
			for j, seq := range seqs {
				tuple[j] = seq[i]
			}
			result[i] = value.FromSlice(tuple)
		}
		return result
	}

	if knownLen >= 0 {
		return value.MakeSizedIterable(zip()), nil
	}
	return value.MakeIterable(func() iter.Seq[value.Value] {
		return func(yield func(value.Value) bool) {
			for _, item := range zip() {
				if !yield(item) {
					return
				}
			}
		}
	}), nil
}

// FilterAbs returns the absolute value of a number.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("abs", FilterAbs)
//
// Template usage:
//
//	{{ -42|abs }}
//	  -> 42
//	{{ 3.14|abs }}
//	  -> 3.14
//
// `filters::abs` (filters.rs:531-549) dispatches on the PAYLOAD, not on what
// converts to a number: a bool or a string cannot be absoluted at all, and an
// integer is absoluted exactly. Asking `AsInt` instead answered 1 for `true`
// and lost precision past 2^53.
func FilterAbs(_ State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	if err := noArgs(args, kwargs); err != nil {
		return value.Undefined(), err
	}
	if val.IsActualFloat() {
		f, _ := val.AsFloat()
		return value.FromFloat(math.Abs(f)), nil
	}
	if val.IsActualInt() {
		i, ok := val.AsBigInt()
		if !ok {
			// ValueRepr::U128 past i128::MAX: already non-negative, returned
			// unchanged.
			return val, nil
		}
		if i.Sign() >= 0 {
			return val, nil
		}
		// i64::MIN widens to i128 rather than erroring; only i128::MIN
		// overflows.
		out, ok := value.FromI128(i.Neg(i))
		if !ok {
			return value.Undefined(), mjerrors.NewError(mjerrors.ErrInvalidOperation, "overflow on abs")
		}
		return out, nil
	}
	return value.Undefined(), mjerrors.NewError(mjerrors.ErrInvalidOperation,
		"cannot get absolute value")
}

// FilterInt converts a value to an integer.
//
// String values are parsed as integers. Float values are truncated.
// Boolean true becomes 1, false becomes 0. If conversion fails, an
// error is returned.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("int", FilterInt)
//
// Template usage:
//
//	{{ "42"|int }}
//	  -> 42
func FilterInt(_ State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	if err := noArgs(args, kwargs); err != nil {
		return value.Undefined(), err
	}
	// `int` and `float` are the two filters that reveal an invalid value's
	// error themselves (filters.rs:580, 600).
	if err, ok := value.InvalidError(val); ok {
		return value.Undefined(), err
	}
	if len(args) > 0 {
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrTooManyArguments, "too many arguments")
	}
	if kwargs.Len() > 0 {
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrTooManyArguments, "too many keyword arguments")
	}

	// `filters::int` (filters.rs:557-588) dispatches on the payload and works
	// in i128 throughout. Both float conversions below are Rust's SATURATING
	// `f64 as i128` cast, which is deterministic; the port used Go's
	// `int64(float64)`, which is implementation-defined out of range and
	// therefore answered differently on arm64 and amd64.
	if val.IsUndefined() || val.IsNone() {
		return value.FromInt(0), nil
	}
	if b, ok := val.AsBool(); ok {
		if b {
			return value.FromInt(1), nil
		}
		return value.FromInt(0), nil
	}
	if val.IsActualInt() {
		return val, nil
	}
	if val.IsActualFloat() {
		f, _ := val.AsFloat()
		out, _ := value.FromI128(value.F64ToI128(f))
		return out, nil
	}
	if s, ok := val.AsString(); ok {
		if i, ok := new(big.Int).SetString(s, 10); ok {
			if out, ok := value.FromI128(i); ok {
				return out, nil
			}
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return value.Undefined(), invalidOp("invalid float literal")
		}
		out, _ := value.FromI128(value.F64ToI128(f))
		return out, nil
	}

	return value.Undefined(), mjerrors.NewError(mjerrors.ErrInvalidOperation, fmt.Sprintf("cannot convert %s to integer", val.Kind()))
}

// FilterFloat converts a value to a float.
//
// String values are parsed as floats. Integer values are converted to floats.
// Boolean true becomes 1.0, false becomes 0.0. If conversion fails, an
// error is returned.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("float", FilterFloat)
//
// Template usage:
//
//	{{ "42.5"|float }}
//	  -> 42.5
func FilterFloat(_ State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	if err := noArgs(args, kwargs); err != nil {
		return value.Undefined(), err
	}
	if err, ok := value.InvalidError(val); ok {
		return value.Undefined(), err
	}
	if len(args) > 0 {
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrTooManyArguments, "too many arguments")
	}
	if kwargs.Len() > 0 {
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrTooManyArguments, "too many keyword arguments")
	}

	if val.IsUndefined() || val.IsNone() {
		return value.FromFloat(0.0), nil
	}
	if f, ok := val.AsFloat(); ok {
		return value.FromFloat(f), nil
	}
	if b, ok := val.AsBool(); ok {
		if b {
			return value.FromFloat(1.0), nil
		}
		return value.FromFloat(0.0), nil
	}
	if s, ok := val.AsString(); ok {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return value.Undefined(), invalidOp("invalid float literal")
		}
		return value.FromFloat(f), nil
	}

	return value.Undefined(), mjerrors.NewError(mjerrors.ErrInvalidOperation, fmt.Sprintf("cannot convert %s to float", val.Kind()))
}

// FilterRound rounds a number to a given precision.
//
// The first parameter specifies the precision (default is 0).
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("round", FilterRound)
//
// Template usage:
//
//	{{ 42.55|round }}
//	  -> 43.0
//	{{ 42.55|round(1) }}
//	  -> 42.6
func FilterRound(_ State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	a := NewArgs(args, kwargs)
	prec, _, err := a.OptInt("i32")
	if err != nil {
		return value.Undefined(), err
	}
	if err := a.Done(); err != nil {
		return value.Undefined(), err
	}
	precision := int(prec)

	if val.IsActualInt() {
		return val, nil
	}

	f, ok := val.AsFloat()
	if !ok {
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrInvalidOperation, fmt.Sprintf("cannot round value (%s)", val.Kind()))
	}

	multiplier := math.Pow(10, float64(precision))
	f = math.Round(f*multiplier) / multiplier

	return value.FromFloat(f), nil
}

// FilterItems returns an iterable of key-value pairs from a map.
//
// This converts a map into a list of [key, value] tuples. The keys are
// sorted alphabetically.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("items", FilterItems)
//
// Template usage:
//
//	<dl>
//	{% for key, value in my_dict|items %}
//	  <dt>{{ key }}
//	  <dd>{{ value }}
//	{% endfor %}
//	</dl>
func FilterItems(_ State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	if err := noArgs(args, kwargs); err != nil {
		return value.Undefined(), err
	}
	// The gate is the value's KIND, and nothing else (filters.rs:363). Whether
	// the mapping can actually be walked is asked INSIDE the iterable, where a
	// map object with no enumerable pairs yields a single invalid value
	// carrying "map is not iterable" rather than making the filter itself fail
	// (filters.rs:365-376).
	if val.Kind() == value.KindMap {
		keys, enumerable := val.MapKeys()
		if !enumerable {
			return value.MakeSizedIterable([]value.Value{
				value.Invalid(mjerrors.NewError(mjerrors.ErrInvalidOperation,
					fmt.Sprintf("%s is not iterable", val.Kind()))),
			}), nil
		}
		// Pairs come out in the mapping's own order, which for an ordered
		// mapping is insertion order: `{% for k, v in m|items %}` is prompt
		// bytes, so this is the same order question as rendering the map.
		m, _ := val.AsMap()

		result := make([]value.Value, 0, len(keys))
		for _, k := range keys {
			item, exists := m[k]
			if !exists {
				item = val.GetItem(value.FromString(k))
			}
			result = append(result, value.FromSlice([]value.Value{
				value.FromString(k),
				item,
			}))
		}
		// `make_object_iterable` (filters.rs:362-379), so the result is an
		// ITERABLE and not a sequence: `dict(a=1)|items is sequence` is false.
		// Its enumerator has an exact size, which is what still gives it a
		// length, an index and the alternate debug layout.
		return value.MakeSizedIterable(result), nil
	}
	return value.Undefined(), mjerrors.NewError(mjerrors.ErrInvalidOperation,
		"cannot convert value into pairs")
}

// FilterDictSort sorts a map by keys or values.
//
// Returns a list of [key, value] pairs sorted by key (default) or by value.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("dictsort", FilterDictSort)
//
// Template usage:
//
//	{% for key, value in my_dict|dictsort %}
//	  {{ key }}: {{ value }}
//	{% endfor %}
//
// Keyword arguments:
//   - by: set to "value" to sort by value instead of key (default: "key")
//   - reverse: set to true to sort in descending order
//   - case_sensitive: set to true for case-sensitive sorting (default: false)
func FilterDictSort(_ State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	// `dictsort(v: &Value, kwargs: Kwargs)` (filters.rs:319-360).
	a := NewArgs(args, kwargs).Kwargs()
	by, _, err := a.GetStr("by")
	if err != nil {
		return value.Undefined(), err
	}
	reverse, _, err := a.GetBool("reverse")
	if err != nil {
		return value.Undefined(), err
	}
	caseSensitive, _, err := a.GetBool("case_sensitive")
	if err != nil {
		return value.Undefined(), err
	}
	if err := a.Done(); err != nil {
		return value.Undefined(), err
	}
	byValue := by == "value"

	// Like `items`, this is a mapping operation: anything that is not a map by
	// KIND is an invalid operation rather than an empty list (filters.rs:319-324).
	if val.Kind() != value.KindMap {
		return value.Undefined(), invalidOp("cannot convert value into pair list")
	}
	// The pairs are the mapping's own iteration, which the engine takes with
	// `ok!(v.try_iter())` (filters.rs:330). For a map object that returns
	// Enumerator::NonEnumerable that FAULTS `map is not iterable` rather than
	// sorting an empty list — so a non-enumerable host map faults here too,
	// through the same generic MapKeys the enumerable path reads. Reaching the
	// values generically (MapKeys + GetItem, not AsMap) is what lets an
	// enumerable host map that is not a Go map — a MapObject class — sort at
	// all, where AsMap declined it. Start from the mapping's own order so that
	// entries the sort considers equal keep it.
	keys, ok := val.MapKeys()
	if !ok {
		return value.Undefined(), invalidOp(fmt.Sprintf("%s is not iterable", val.Kind()))
	}
	get := func(k string) value.Value { return val.GetItem(value.FromString(k)) }

	// `dictsort` is the third caller of the engine's one comparator
	// (filters.rs:333-336), so it sorts by exactly what `sort` and
	// `groupby` do — Unicode case folding, and a tie that stays a tie.
	if byValue {
		sort.SliceStable(keys, func(i, j int) bool {
			cmp := cmpHelper(get(keys[i]), get(keys[j]), caseSensitive)
			if reverse {
				return cmp > 0
			}
			return cmp < 0
		})
	} else {
		sort.SliceStable(keys, func(i, j int) bool {
			cmp := cmpHelper(value.FromString(keys[i]), value.FromString(keys[j]), caseSensitive)
			if reverse {
				return cmp > 0
			}
			return cmp < 0
		})
	}

	var result []value.Value
	for _, k := range keys {
		result = append(result, value.FromSlice([]value.Value{
			value.FromString(k),
			get(k),
		}))
	}
	return value.FromSlice(result), nil
}

// FilterAttr looks up an attribute by name.
//
// This is equivalent to using the [] operator in MiniJinja. It's provided
// for compatibility with Jinja2.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("attr", FilterAttr)
//
// Template usage:
//
//	{{ value|attr("key") }}
//	  -> same as value["key"] or value.key
func FilterAttr(_ State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	a := NewArgs(args, kwargs)
	key, err := a.Value()
	if err != nil {
		return value.Undefined(), err
	}
	if err := a.Done(); err != nil {
		return value.Undefined(), err
	}
	// `attr` is the `[]` operator (filters.rs:645-647), so a non-string key is
	// a lookup by that value rather than an error.
	return val.GetItem(key), nil
}

// FilterIndent indents each line of a string with spaces.
//
// The first parameter sets the indentation width (default: 4). The second
// optional parameter determines whether to indent the first line (default: false).
// The third optional parameter determines whether to indent blank lines (default: false).
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("indent", FilterIndent)
//
// Template usage:
//
//	config:
//	{{ yaml_content|indent(2) }}
//	{{ yaml_content|indent(2, true) }}
//	{{ yaml_content|indent(2, true, true) }}
func FilterIndent(_ State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	// `indent(value: String, width: usize, indent_first_line: Option<bool>,
	// indent_blank_lines: Option<bool>)` (filters.rs:1066-1101): the subject is
	// stringified and the width is required.
	s, err := stringArg(val)
	if err != nil {
		return value.Undefined(), err
	}
	a := NewArgs(args, kwargs)
	w, err := a.Usize()
	if err != nil {
		return value.Undefined(), err
	}
	first, err := a.OptBool()
	if err != nil {
		return value.Undefined(), err
	}
	blank, err := a.OptBool()
	if err != nil {
		return value.Undefined(), err
	}
	if err := a.Done(); err != nil {
		return value.Undefined(), err
	}
	width, err := allocSize(w, "width")
	if err != nil {
		return value.Undefined(), err
	}

	// The engine strips a terminal line ending BOTH before indenting and after
	// (filters.rs:1067-1099), so `'a\n'|indent(2)` is `"a"` and not `"a\n"`.
	// Each strip removes one '\n' and then one '\r', independently: `'a\r'`
	// loses its CR even though no LF preceded it.
	stripTrailingNewline := func(in string) string {
		in = strings.TrimSuffix(in, "\n")
		return strings.TrimSuffix(in, "\r")
	}

	indent := strings.Repeat(" ", width)
	lines := strings.Split(stripTrailingNewline(s), "\n")

	var out strings.Builder
	if !first {
		// The first line is emitted verbatim and is never treated as blank.
		out.WriteString(lines[0])
		out.WriteByte('\n')
		lines = lines[1:]
	}
	for _, line := range lines {
		if line == "" {
			if blank {
				out.WriteString(indent)
			}
		} else {
			out.WriteString(indent)
			out.WriteString(line)
		}
		out.WriteByte('\n')
	}
	return value.FromString(stripTrailingNewline(out.String())), nil
}

// FilterPprint pretty-prints a value for debugging.
//
// This is useful for debugging templates as it formats values in a more
// readable way than the default string representation.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("pprint", FilterPprint)
//
// Template usage:
//
//	<pre>{{ complex_object|pprint }}</pre>
func FilterPprint(_ State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	if err := noArgs(args, kwargs); err != nil {
		return value.Undefined(), err
	}
	return value.FromString(pprintValue(val, 0)), nil
}

// pprintValue is `{value:#?}` (filters.rs:1665-1667), the ALTERNATE debug form
// of a value.
//
// It is the same renderer the compact `Repr` goes through, selected the same
// way (value/object.rs:327-353): a map is a `debug_map`, and a sequence OR an
// iterable is a `debug_list` — but only when its enumerator has an exact
// length, because the engine will not risk iterating an unsized object just to
// print it. Anything else falls back to the object's own debug form.
//
// So the choice is about the object's REPRESENTATION and its enumerator, not
// about whether the fork happens to hold a Go slice: `groupby(...)` records and
// the pycompat views are objects, and printing them as `[]` was reading a slice
// that was never there.
func pprintValue(val value.Value, indent int) string {
	pad := strings.Repeat(" ", indent)

	// The engine's `{:#?}` of an object calls its `render` (value/mod.rs:462),
	// and a CUSTOM render (an object with its own string form) wins over the
	// default `debug_map`/`debug_list`. So an alias-aware class or a bare enum
	// prints its own render here rather than a map rebuilt from its canonical
	// keys — the same object render `{{ x }}` and `Repr` already dispatch. Rust
	// re-indents a nested entry's own newlines by the surrounding depth
	// (DebugList/DebugMap's PadAdapter); reproduce that by shifting every line
	// after the first by this indent, which is a no-op at the top and for a
	// single-line render.
	if s, ok := val.ObjectRender(); ok {
		return strings.ReplaceAll(s, "\n", "\n"+pad)
	}

	switch val.Kind() {
	case value.KindSeq, value.KindIterable:
		if _, sized := val.Len(); !sized {
			return val.Repr()
		}
		items := val.Iter()
		if len(items) == 0 {
			return "[]"
		}
		var sb strings.Builder
		sb.WriteString("[\n")
		for _, item := range items {
			sb.WriteString(strings.Repeat(" ", indent+4))
			sb.WriteString(pprintValue(item, indent+4))
			sb.WriteString(",\n")
		}
		sb.WriteString(pad)
		sb.WriteString("]")
		return sb.String()
	case value.KindMap:
		// The mapping's own order, so a pretty-printed map does not disagree
		// with the same map rendered normally.
		keys, ok := val.MapKeys()
		if !ok {
			return val.Repr()
		}
		if len(keys) == 0 {
			return "{}"
		}
		// A key is spelled by its own debug form, not quoted unconditionally:
		// the engine's `debug_map` writes `entry(&key, &value)` with `key` a
		// `Value` (value/object.rs:333-338), so `{1: 'a'}` is `{1: "a"}` and
		// `{true: 'a'}` is `{true: "a"}`. This fork keys its mappings by string
		// and remembers a key's spelling beside it (patches #37 and #81), which
		// is what [value.OrderedMap.KeyRepr] reads; quoting the string form
		// instead made every non-string key `"1"`. KeyRepr is nil-safe and
		// falls back to the string's own debug form, which is what a mapping
		// with no remembered spelling has always printed.
		ordered, _ := val.AsOrderedMap()
		var sb strings.Builder
		sb.WriteString("{\n")
		for _, k := range keys {
			sb.WriteString(strings.Repeat(" ", indent+4))
			sb.WriteString(fmt.Sprintf("%s: %s,",
				ordered.KeyRepr(k),
				pprintValue(val.GetItem(value.FromString(k)), indent+4)))
			sb.WriteString("\n")
		}
		sb.WriteString(pad)
		sb.WriteString("}")
		return sb.String()
	default:
		return val.Repr()
	}
}

// FilterTojson serializes a value to JSON.
//
// The resulting value is safe to use in HTML as special characters are escaped.
// The optional parameter controls indentation: true for 2 spaces, or an integer
// for custom indentation.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("tojson", FilterTojson)
//
// Template usage:
//
//	<script>
//	  const CONFIG = {{ config|tojson }};
//	</script>
//	<a href="#" data-info='{{ info|tojson }}'>...</a>
//	{{ data|tojson(indent=2) }}
//
// Keyword arguments:
//   - indent: true for 2-space indent, or integer for custom indent
func FilterTojson(_ State, val value.Value, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	// `tojson(value, indent: Option<Value>, kwargs)`: `true` means two spaces,
	// `false` means compact, and a number is a literal width
	// (filters.rs:1007-1020).
	indentArg := value.Undefined()
	switch {
	case len(args) > 1:
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrTooManyArguments, "too many arguments")
	case len(args) == 1:
		indentArg = args[0]
	}
	// The Kwargs parameter is consumed AFTER the positional one is filled, so
	// `tojson(2, indent=2)` leaves `indent` unused rather than being an arity
	// error: the engine reports the unknown keyword (value/argtypes.rs
	// assert_all_used), not "too many arguments".
	unused := make([]string, 0, kwargs.Len())
	for _, name := range kwargs.Keys() {
		if name == "indent" && len(args) == 0 {
			indentArg, _ = kwargs.Get(name)
			continue
		}
		unused = append(unused, name)
	}
	if len(unused) > 0 {
		// In keyword order, which is the engine's: the first unused one.
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrTooManyArguments,
			fmt.Sprintf("unknown keyword argument '%s'", unused[0]))
	}

	indent := -1
	if !indentArg.IsUndefined() && !indentArg.IsNone() {
		if indentArg.Kind() == value.KindBool {
			if b, _ := indentArg.AsBool(); b {
				indent = 2
			}
		} else {
			// `usize::try_from(val)` (filters.rs:1017), the same ArgType the
			// other usize parameters declare, so the upper half of u64 converts.
			n, err := ConvertUsize(indentArg)
			if err != nil {
				return value.Undefined(), err
			}
			indent, err = allocSize(n, "indent")
			if err != nil {
				return value.Undefined(), err
			}
		}
	}

	encoded, err := serdejson.Marshal(val, indent)
	if err != nil {
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrInvalidOperation, "cannot serialize to JSON")
	}
	return value.FromSafeString(serdejson.EscapeForHTML(encoded)), nil
}

func urlencodeString(input string) string {
	escaped := url.QueryEscape(input)
	escaped = strings.ReplaceAll(escaped, "+", "%20")
	escaped = strings.ReplaceAll(escaped, "%2F", "/")
	return escaped
}

// FilterUrlencode URL-encodes a value.
//
// If given a map, it encodes the parameters into a query string. Otherwise,
// it encodes the stringified value. None and undefined values in maps are
// skipped.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddFilter("urlencode", FilterUrlencode)
//
// Template usage:
//
//	<a href="/search?{{ {"q": "my search", "lang": "en"}|urlencode }}">
//	{{ "hello world"|urlencode }}
//	  -> "hello%20world"
func FilterUrlencode(_ State, val value.Value, _ []value.Value, _ *value.OrderedMap) (value.Value, error) {
	// A mapping encodes as a query string. The gate is the value's KIND
	// (filters.rs:1122), and the pairs come from `ok!(value.try_iter())`
	// (filters.rs:1123) — so a map object that cannot enumerate its pairs FAULTS
	// `map is not iterable` exactly as `items`/`dictsort` do, rather than falling
	// through to string coercion and succeeding (the out-do a non-enumerable map
	// took through the old `AsMap` gate). An enumerable host map that AsMap does
	// not recognise is reached generically, through MapKeys + GetItem, in the
	// map's own order — not a Go-map sort.
	//
	// This filter is withdrawn from the default environment (defaults.go:24, it
	// is gated behind an engine feature BAML does not enable), so the gate is not
	// reachable through a template today; the parity is kept for any external
	// caller that registers it, and so the value model never carries a latent
	// succeed-on-non-enumerable path.
	if val.Kind() == value.KindMap {
		keys, ok := val.MapKeys()
		if !ok {
			return value.Undefined(), invalidOp(fmt.Sprintf("%s is not iterable", val.Kind()))
		}
		var parts []string
		for _, k := range keys {
			v := val.GetItem(value.FromString(k))
			// none AND undefined are both skipped (filters.rs:1126).
			if v.IsNone() || v.IsUndefined() {
				continue
			}
			parts = append(parts, urlencodeString(k)+"="+urlencodeString(v.String()))
		}
		return value.FromString(strings.Join(parts, "&")), nil
	}

	s, ok := val.AsString()
	if !ok {
		s = val.String()
	}
	return value.FromString(urlencodeString(s)), nil
}
