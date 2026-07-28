// Package filters provides MiniJinja's built-in filters.
package filters

import (
	"context"
	"fmt"
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
type FilterFunc func(state State, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error)

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
func FilterUpper(_ State, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
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
func noArgs(args []value.Value, kwargs map[string]value.Value) error {
	if len(args) > 0 || len(kwargs) > 0 {
		return mjerrors.NewError(mjerrors.ErrTooManyArguments, "received too many arguments")
	}
	return nil
}

// maxArgs rejects more than n positional arguments.
func maxArgs(args []value.Value, kwargs map[string]value.Value, n int) error {
	if len(args) > n || len(kwargs) > 0 {
		return mjerrors.NewError(mjerrors.ErrTooManyArguments, "received too many arguments")
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
func tryIter(val value.Value, msg string) ([]value.Value, error) {
	switch val.Kind() {
	case value.KindSeq, value.KindMap, value.KindIterable, value.KindString:
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
func FilterLower(_ State, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
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
func FilterCapitalize(_ State, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
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
func FilterTitle(_ State, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
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
func FilterTrim(_ State, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
	if len(kwargs) > 0 {
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrTooManyArguments, "received too many arguments")
	}
	s, err := stringArg(val)
	if err != nil {
		return value.Undefined(), err
	}
	if len(args) > 1 {
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrTooManyArguments, "received too many arguments")
	}
	if len(args) == 1 && !args[0].IsNone() && !args[0].IsUndefined() {
		chars, err := stringArg(args[0])
		if err != nil {
			return value.Undefined(), err
		}
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
func FilterReplace(_ State, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
	if len(kwargs) > 0 {
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrTooManyArguments, "received too many arguments")
	}
	s, err := stringArg(val)
	if err != nil {
		return value.Undefined(), err
	}
	if len(args) < 2 {
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrMissingArgument, "missing argument")
	}
	if len(args) > 2 {
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrTooManyArguments, "received too many arguments")
	}
	from, err := stringArg(args[0])
	if err != nil {
		return value.Undefined(), err
	}
	to, err := stringArg(args[1])
	if err != nil {
		return value.Undefined(), err
	}
	return value.FromString(strings.ReplaceAll(s, from, to)), nil
}

// FilterFormat applies printf-style formatting to a string.
//
// Example:
//
//	{{ "%s, %s!"|format(greeting, name) }}
func FilterFormat(_ State, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
	formatStr, ok := val.AsString()
	if !ok {
		// `&str`, so a non-string format string is an error rather than being
		// stringified (argtypes.rs:469-480).
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrInvalidOperation, "value is not a string")
	}
	if len(kwargs) > 0 {
		// `Rest<Value>` collects positional arguments only; a keyword
		// argument arrives as a trailing Kwargs value and is not consumed.
		args = append(append([]value.Value(nil), args...), value.FromMap(kwargs))
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
func FilterDefault(_ State, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
	if len(kwargs) > 0 {
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrTooManyArguments, "too many keyword arguments")
	}

	if val.IsUndefined() {
		if len(args) > 0 {
			return args[0], nil
		}
		return value.FromString(""), nil
	}

	// Check boolean flag for empty check
	checkBool := false
	if len(args) > 1 {
		if b, ok := args[1].AsBool(); ok {
			checkBool = b
		}
	}

	if checkBool && !val.IsTrue() {
		if len(args) > 0 {
			return args[0], nil
		}
		return value.FromString(""), nil
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
func FilterSafe(_ State, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
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
func FilterEscape(_ State, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
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
func FilterString(_ State, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
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
func FilterBool(state State, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
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
func FilterSplit(_ State, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
	if len(kwargs) > 0 {
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrTooManyArguments, "received too many arguments")
	}
	s, ok := val.AsString()
	if !ok {
		// The engine's argument type here is `Arc<str>`, which — unlike the
		// `Cow<'_, str>` the casing filters take — requires a real string
		// (argtypes.rs:482-517).
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrInvalidOperation, "value is not a string")
	}
	if len(args) > 2 {
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrTooManyArguments, "received too many arguments")
	}

	var sep string
	var hasSep bool
	if len(args) > 0 && !args[0].IsNone() && !args[0].IsUndefined() {
		sep, hasSep = args[0].AsString()
		if !hasSep {
			return value.Undefined(), mjerrors.NewError(mjerrors.ErrInvalidOperation, "value is not a string")
		}
	}

	var maxSplits int64
	var hasMax bool
	if len(args) > 1 && !args[1].IsNone() && !args[1].IsUndefined() {
		maxSplits, hasMax = args[1].AsInt()
		if !hasMax || !args[1].IsActualInt() {
			return value.Undefined(), mjerrors.NewError(mjerrors.ErrInvalidOperation,
				fmt.Sprintf("cannot convert %s to i64", args[1].Kind()))
		}
	}

	parts := Split(s, sep, hasSep, maxSplits, hasMax)
	result := make([]value.Value, len(parts))
	for i, p := range parts {
		result[i] = value.FromString(p)
	}
	return value.FromIterator(value.NewIterator("split", result)), nil
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
func FilterLines(_ State, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
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
func FilterLength(_ State, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
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
func FilterFirst(_ State, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
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
	switch val.Kind() {
	case value.KindSeq, value.KindMap, value.KindIterable, value.KindPlain:
		items := val.Iter()
		if len(items) > 0 {
			return items[0], nil
		}
		return value.Undefined(), nil
	default:
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrInvalidOperation,
			"cannot get first item from value")
	}
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
func FilterLast(_ State, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
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
	switch val.Kind() {
	case value.KindSeq, value.KindIterable, value.KindMap, value.KindPlain:
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
func FilterReverse(_ State, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
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
	// enumerated at all is an error (value/mod.rs:1400-1440).
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
		// container/map-reverse-twice).
		return value.FromIterator(value.NewIterator("reversed", val.Iter())), nil
	case value.KindSeq, value.KindIterable, value.KindPlain:
		items := val.Iter()
		result := make([]value.Value, len(items))
		for i, item := range items {
			result[len(items)-1-i] = item
		}
		return value.FromIterator(value.NewIterator("reversed", result)), nil
	default:
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrInvalidOperation,
			fmt.Sprintf("cannot reverse value of type %s", val.Kind()))
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
func FilterSort(state State, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
	items := val.Iter()
	if items == nil {
		return val, nil
	}

	if len(args) > 0 {
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrTooManyArguments, "too many arguments")
	}

	reverse := false
	if r, ok := kwargs["reverse"]; ok {
		if b, ok := r.AsBool(); ok {
			reverse = b
		}
	}

	caseSensitive := false
	if cs, ok := kwargs["case_sensitive"]; ok {
		if b, ok := cs.AsBool(); ok {
			caseSensitive = b
		}
	}

	// Get attribute for sorting
	var attrName string
	if attr, ok := kwargs["attribute"]; ok {
		if s, ok := attr.AsString(); ok {
			attrName = s
		}
	}

	result := make([]value.Value, len(items))
	copy(result, items)

	sort.SliceStable(result, func(i, j int) bool {
		a, b := result[i], result[j]

		// Apply attribute if specified
		if attrName != "" {
			a = getDeepAttr(a, attrName)
			b = getDeepAttr(b, attrName)
		}

		// Case-insensitive string comparison
		if !caseSensitive {
			if s1, ok1 := a.AsString(); ok1 {
				if s2, ok2 := b.AsString(); ok2 {
					cmp := strings.Compare(strings.ToLower(s1), strings.ToLower(s2))
					if reverse {
						return cmp > 0
					}
					return cmp < 0
				}
			}
		}

		cmp, ok := a.Compare(b)
		if !ok {
			return false
		}
		if reverse {
			return cmp > 0
		}
		return cmp < 0
	})

	return value.FromSlice(result), nil
}

// getDeepAttr gets a nested attribute (supports "a.b.0" syntax)
func getDeepAttr(v value.Value, path string) value.Value {
	parts := strings.Split(path, ".")
	for _, part := range parts {
		// Try as integer index first
		if idx, err := parseInt(part); err == nil {
			v = v.GetItem(value.FromInt(idx))
		} else {
			v = v.GetAttr(part)
		}
		if v.IsUndefined() {
			return v
		}
	}
	return v
}

func parseInt(s string) (int64, error) {
	var n int64
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
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
func FilterJoin(_ State, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
	if len(kwargs) > 0 {
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrTooManyArguments, "received too many arguments")
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

	sep := ""
	if len(args) > 0 {
		sep, err = stringArg(args[0])
		if err != nil {
			return value.Undefined(), err
		}
	}
	if len(args) > 1 {
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrTooManyArguments, "received too many arguments")
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
func FilterList(state State, val value.Value, _ []value.Value, _ map[string]value.Value) (value.Value, error) {
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
func FilterUnique(_ State, val value.Value, _ []value.Value, kwargs map[string]value.Value) (value.Value, error) {
	items := val.Iter()
	if items == nil {
		return val, nil
	}

	caseSensitive := false
	if cs, ok := kwargs["case_sensitive"]; ok {
		if b, ok := cs.AsBool(); ok {
			caseSensitive = b
		}
	}

	attrName := ""
	if attr, ok := kwargs["attribute"]; ok {
		if s, ok := attr.AsString(); ok {
			attrName = s
		} else {
			return value.Undefined(), mjerrors.NewError(mjerrors.ErrInvalidOperation, "attribute must be a string")
		}
	}

	seen := make(map[string]bool)
	var result []value.Value
	for _, item := range items {
		valueToCompare := item
		if attrName != "" {
			valueToCompare = getDeepAttr(item, attrName)
		}

		var key string
		if !caseSensitive {
			if s, ok := valueToCompare.AsString(); ok {
				key = strings.ToLower(s)
			} else {
				key = valueToCompare.Repr()
			}
		} else {
			key = valueToCompare.Repr()
		}
		if !seen[key] {
			seen[key] = true
			result = append(result, item)
		}
	}
	return value.FromSlice(result), nil
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
func FilterMin(_ State, val value.Value, _ []value.Value, _ map[string]value.Value) (value.Value, error) {
	items := val.Iter()
	if items == nil || len(items) == 0 {
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
func FilterMax(_ State, val value.Value, _ []value.Value, _ map[string]value.Value) (value.Value, error) {
	items := val.Iter()
	if items == nil || len(items) == 0 {
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
func FilterSum(_ State, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
	if len(args) > 0 || len(kwargs) > 0 {
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrTooManyArguments, "received too many arguments")
	}
	items, err := tryIter(val, fmt.Sprintf("cannot convert %s to an iterator", val.Kind()))
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
func FilterBatch(_ State, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
	if len(kwargs) > 0 {
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrTooManyArguments, "too many keyword arguments")
	}
	items, err := tryIter(val, fmt.Sprintf("cannot convert %s to an iterator", val.Kind()))
	if err != nil {
		return value.Undefined(), err
	}

	lineCount, err := countArg(args, 0)
	if err != nil {
		return value.Undefined(), err
	}

	fillWith := value.Undefined()
	if len(args) > 1 {
		fillWith = args[1]
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
// countArg is the `count: usize` argument `batch` and `slice` take: it must be
// present, non-negative and non-zero (filters.rs:945-953).
func countArg(args []value.Value, i int) (int, error) {
	if i >= len(args) {
		return 0, mjerrors.NewError(mjerrors.ErrMissingArgument, "missing argument")
	}
	n, ok := args[i].AsInt()
	if !ok || !args[i].IsActualInt() || n < 0 {
		return 0, mjerrors.NewError(mjerrors.ErrInvalidOperation,
			fmt.Sprintf("cannot convert %s to usize", args[i].Kind()))
	}
	if n == 0 {
		return 0, mjerrors.NewError(mjerrors.ErrInvalidOperation, "count cannot be 0")
	}
	return int(n), nil
}

func FilterSlice(_ State, val value.Value, args []value.Value, _ map[string]value.Value) (value.Value, error) {
	items, err := tryIter(val, fmt.Sprintf("cannot convert %s to an iterator", val.Kind()))
	if err != nil {
		return value.Undefined(), err
	}

	sliceCount, err := countArg(args, 0)
	if err != nil {
		return value.Undefined(), err
	}

	fillWith := value.Undefined()
	if len(args) > 1 {
		fillWith = args[1]
	}

	// Calculate slice sizes
	length := len(items)
	baseSize := length / sliceCount
	remainder := length % sliceCount
	maxSize := baseSize
	if remainder > 0 {
		maxSize++
	}

	var result []value.Value
	offset := 0
	for i := 0; i < sliceCount; i++ {
		size := baseSize
		if i < remainder {
			size++
		}

		slice := make([]value.Value, size)
		copy(slice, items[offset:offset+size])

		// Fill slices to the maximum size when requested
		if !fillWith.IsUndefined() && len(slice) < maxSize {
			for len(slice) < maxSize {
				slice = append(slice, fillWith)
			}
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
func FilterMap(state State, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
	items := val.Iter()
	if items == nil {
		return val, nil
	}

	// Check for filter name as first positional arg
	var filterName string
	if len(args) > 0 {
		if s, ok := args[0].AsString(); ok {
			filterName = s
		}
	}

	// Get attribute name
	var attrName string
	attrValue := value.Undefined()
	if attr, ok := kwargs["attribute"]; ok {
		attrValue = attr
		if s, ok := attr.AsString(); ok {
			attrName = s
		}
	}

	// Get default value
	defaultVal := value.Undefined()
	if def, ok := kwargs["default"]; ok {
		defaultVal = def
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
				return value.Undefined(), mjerrors.NewError(mjerrors.ErrUnknownFilter, filterName)
			}
			var err error
			mapped, err = filterFn(state, item, args[1:], kwargs)
			if err != nil {
				return val, err
			}
		} else {
			return value.Undefined(), mjerrors.NewError(mjerrors.ErrMissingArgument, "missing argument")
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
func FilterSelect(state State, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
	items := val.Iter()
	if items == nil {
		return val, nil
	}

	// Get test name if provided
	var testName string
	if len(args) > 0 {
		if s, ok := args[0].AsString(); ok {
			testName = normalizeTestName(s)
		}
	}

	var result []value.Value
	for _, item := range items {
		var keep bool
		if testName != "" {
			testFn, ok := state.GetTest(testName)
			if !ok {
				return value.Undefined(), mjerrors.NewError(mjerrors.ErrUnknownTest, testName)
			}
			var err error
			keep, err = testFn(state, item, args[1:])
			if err != nil {
				return val, err
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
func FilterReject(state State, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
	items := val.Iter()
	if items == nil {
		return val, nil
	}

	// Get test name if provided
	var testName string
	if len(args) > 0 {
		if s, ok := args[0].AsString(); ok {
			testName = normalizeTestName(s)
		}
	}

	var result []value.Value
	for _, item := range items {
		var reject bool
		if testName != "" {
			testFn, ok := state.GetTest(testName)
			if !ok {
				return value.Undefined(), mjerrors.NewError(mjerrors.ErrUnknownTest, testName)
			}
			var err error
			reject, err = testFn(state, item, args[1:])
			if err != nil {
				return val, err
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
func FilterSelectAttr(state State, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
	items := val.Iter()
	if items == nil {
		return val, nil
	}

	if len(args) < 1 {
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrMissingArgument, "missing argument")
	}
	attrName, _ := args[0].AsString()

	// Get test name if provided (second arg)
	var testName string
	if len(args) > 1 {
		if s, ok := args[1].AsString(); ok {
			testName = normalizeTestName(s)
		}
	}

	var result []value.Value
	for _, item := range items {
		attr := item.GetAttr(attrName)
		var keep bool
		if testName != "" {
			testFn, ok := state.GetTest(testName)
			if !ok {
				return value.Undefined(), mjerrors.NewError(mjerrors.ErrUnknownTest, testName)
			}
			var err error
			keep, err = testFn(state, attr, args[2:])
			if err != nil {
				return val, err
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
func FilterRejectAttr(state State, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
	items := val.Iter()
	if items == nil {
		return val, nil
	}

	if len(args) < 1 {
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrMissingArgument, "missing argument")
	}
	attrName, _ := args[0].AsString()

	// Get test name if provided (second arg)
	var testName string
	if len(args) > 1 {
		if s, ok := args[1].AsString(); ok {
			testName = normalizeTestName(s)
		}
	}

	var result []value.Value
	for _, item := range items {
		attr := item.GetAttr(attrName)
		var reject bool
		if testName != "" {
			testFn, ok := state.GetTest(testName)
			if !ok {
				return value.Undefined(), mjerrors.NewError(mjerrors.ErrUnknownTest, testName)
			}
			var err error
			reject, err = testFn(state, attr, args[2:])
			if err != nil {
				return val, err
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
func FilterGroupBy(_ State, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
	items := val.Iter()
	if items == nil {
		return val, nil
	}

	// Get attribute name
	var attrName string
	if len(args) > 0 {
		if s, ok := args[0].AsString(); ok {
			attrName = s
		}
	}
	if attr, ok := kwargs["attribute"]; ok {
		if s, ok := attr.AsString(); ok {
			attrName = s
		}
	}
	if attrName == "" {
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrMissingArgument, "missing argument")
	}

	// Get default value
	defaultVal := value.Undefined()
	if def, ok := kwargs["default"]; ok {
		defaultVal = def
	}
	if len(args) > 1 && defaultVal.IsUndefined() {
		defaultVal = args[1]
	}

	// Case sensitivity
	caseSensitive := false
	if cs, ok := kwargs["case_sensitive"]; ok {
		if b, ok := cs.AsBool(); ok {
			caseSensitive = b
		}
	}

	// Sort items by group key
	sorted := make([]value.Value, len(items))
	copy(sorted, items)

	sort.SliceStable(sorted, func(i, j int) bool {
		left := groupByValue(sorted[i], attrName, defaultVal)
		right := groupByValue(sorted[j], attrName, defaultVal)
		return compareGroupBy(left, right, caseSensitive) < 0
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

		if !groupByEqual(currentGrouper, groupValue, caseSensitive) {
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

func compareGroupBy(a, b value.Value, caseSensitive bool) int {
	if !caseSensitive {
		if s1, ok := a.AsString(); ok {
			if s2, ok := b.AsString(); ok {
				lowerCmp := strings.Compare(strings.ToLower(s1), strings.ToLower(s2))
				if lowerCmp != 0 {
					return lowerCmp
				}
				if s1 != s2 {
					rank1 := caseRank(s1)
					rank2 := caseRank(s2)
					if rank1 != rank2 {
						if rank1 < rank2 {
							return -1
						}
						return 1
					}
					return strings.Compare(s1, s2)
				}
				return 0
			}
		}
	}
	if cmp, ok := a.Compare(b); ok {
		return cmp
	}
	return strings.Compare(a.Repr(), b.Repr())
}

func groupByEqual(a, b value.Value, caseSensitive bool) bool {
	if !caseSensitive {
		if s1, ok := a.AsString(); ok {
			if s2, ok := b.AsString(); ok {
				return strings.EqualFold(s1, s2)
			}
		}
	}
	if cmp, ok := a.Compare(b); ok {
		return cmp == 0
	}
	return a.Repr() == b.Repr()
}

func caseRank(s string) int {
	if s == strings.ToLower(s) {
		return 1
	}
	return 0
}

// groupObject represents a group in groupby filter
type groupObject struct {
	grouper value.Value
	list    []value.Value
}

func (g *groupObject) GetAttr(name string) value.Value {
	switch name {
	case "grouper":
		return g.grouper
	case "list":
		return value.FromSlice(g.list)
	}
	return value.Undefined()
}

func (g *groupObject) Iter() []value.Value {
	return []value.Value{g.grouper, value.FromSlice(g.list)}
}

func (g *groupObject) Len() (int, bool) {
	return 2, true
}

func (g *groupObject) GetItem(key value.Value) value.Value {
	if idx, ok := key.AsInt(); ok {
		switch idx {
		case 0:
			return g.grouper
		case 1:
			return value.FromSlice(g.list)
		}
	}
	return value.Undefined()
}

func (g *groupObject) String() string {
	listRepr := value.FromSlice(g.list).Repr()
	return fmt.Sprintf("[%s, %s]", g.grouper.Repr(), listRepr)
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
func FilterChain(_ State, val value.Value, args []value.Value, _ map[string]value.Value) (value.Value, error) {
	allValues := append([]value.Value{val}, args...)

	allMaps := true
	allSeq := true
	for _, v := range allValues {
		if _, ok := v.AsMap(); !ok {
			allMaps = false
		}
		if _, ok := v.AsSlice(); !ok {
			allSeq = false
		}
	}

	if allMaps {
		merged := make(map[string]value.Value)
		for _, v := range allValues {
			m, _ := v.AsMap()
			for k, val := range m {
				merged[k] = val
			}
		}
		return value.FromMap(merged), nil
	}

	// Get items from first value
	items := val.Iter()
	if items == nil {
		items = []value.Value{}
	}

	// Chain all arguments
	for _, arg := range args {
		argItems := arg.Iter()
		if argItems != nil {
			items = append(items, argItems...)
		}
	}

	if allSeq {
		return value.FromSlice(items), nil
	}

	// Return as iterable to support length, indexing, etc.
	return value.FromObject(&chainObject{items: items}), nil
}

// chainObject allows chained iterables to support length and indexing
type chainObject struct {
	items []value.Value
}

func (c *chainObject) GetAttr(name string) value.Value {
	return value.Undefined()
}

func (c *chainObject) Iter() []value.Value {
	return c.items
}

func (c *chainObject) Len() (int, bool) {
	return len(c.items), true
}

func (c *chainObject) GetItem(key value.Value) value.Value {
	if idx, ok := key.AsInt(); ok {
		if idx < 0 {
			idx = int64(len(c.items)) + idx
		}
		if idx >= 0 && idx < int64(len(c.items)) {
			return c.items[idx]
		}
	}
	return value.Undefined()
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
func FilterZip(_ State, val value.Value, args []value.Value, _ map[string]value.Value) (value.Value, error) {
	// Collect all sequences
	seqs := [][]value.Value{val.Iter()}
	for _, arg := range args {
		seqs = append(seqs, arg.Iter())
	}

	// Find minimum length
	minLen := math.MaxInt
	for _, seq := range seqs {
		if seq == nil {
			minLen = 0
			break
		}
		if len(seq) < minLen {
			minLen = len(seq)
		}
	}

	if minLen == 0 || minLen == math.MaxInt {
		return value.FromSlice(nil), nil
	}

	// Zip
	result := make([]value.Value, minLen)
	for i := 0; i < minLen; i++ {
		tuple := make([]value.Value, len(seqs))
		for j, seq := range seqs {
			tuple[j] = seq[i]
		}
		result[i] = value.FromSlice(tuple)
	}
	return value.FromSlice(result), nil
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
func FilterAbs(_ State, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
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
func FilterInt(_ State, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
	if err := noArgs(args, kwargs); err != nil {
		return value.Undefined(), err
	}
	if len(args) > 0 {
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrTooManyArguments, "too many arguments")
	}
	if len(kwargs) > 0 {
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
			return value.Undefined(), mjerrors.NewError(mjerrors.ErrInvalidOperation, err.Error())
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
func FilterFloat(_ State, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
	if err := noArgs(args, kwargs); err != nil {
		return value.Undefined(), err
	}
	if len(args) > 0 {
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrTooManyArguments, "too many arguments")
	}
	if len(kwargs) > 0 {
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
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return value.FromFloat(f), nil
		} else {
			return value.Undefined(), mjerrors.NewError(mjerrors.ErrInvalidOperation, err.Error())
		}
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
func FilterRound(_ State, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
	if len(args) > 1 {
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrTooManyArguments, "too many arguments")
	}
	if len(kwargs) > 0 {
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrTooManyArguments, "too many keyword arguments")
	}

	if val.IsActualInt() {
		return val, nil
	}

	f, ok := val.AsFloat()
	if !ok {
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrInvalidOperation, fmt.Sprintf("cannot round value (%s)", val.Kind()))
	}

	precision := 0
	if len(args) > 0 {
		if p, ok := args[0].AsInt(); ok {
			precision = int(p)
		} else {
			return value.Undefined(), mjerrors.NewError(mjerrors.ErrInvalidOperation, "precision must be an integer")
		}
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
func FilterItems(_ State, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
	if err := noArgs(args, kwargs); err != nil {
		return value.Undefined(), err
	}
	if m, ok := val.AsMap(); ok {
		// Pairs come out in the mapping's own order, which for an ordered
		// mapping is insertion order: `{% for k, v in m|items %}` is prompt
		// bytes, so this is the same order question as rendering the map.
		keys, ok := val.MapKeys()
		if !ok {
			keys = make([]string, 0, len(m))
			for k := range m {
				keys = append(keys, k)
			}
			sort.Strings(keys)
		}

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
		return value.FromSlice(result), nil
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
func FilterDictSort(_ State, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
	if m, ok := val.AsMap(); ok {
		// Start from the mapping's own order so that entries the sort considers
		// equal keep it, rather than coming out in Go map order.
		keys, ok := val.MapKeys()
		if !ok {
			keys = make([]string, 0, len(m))
			for k := range m {
				keys = append(keys, k)
			}
		}

		if len(args) > 0 {
			return value.Undefined(), mjerrors.NewError(mjerrors.ErrTooManyArguments, "too many arguments")
		}

		byValue := false
		if b, ok := kwargs["by"]; ok {
			if s, ok := b.AsString(); ok && s == "value" {
				byValue = true
			}
		}

		reverse := false
		if b, ok := kwargs["reverse"]; ok {
			if bb, ok := b.AsBool(); ok {
				reverse = bb
			}
		}

		caseSensitive := false
		if cs, ok := kwargs["case_sensitive"]; ok {
			if b, ok := cs.AsBool(); ok {
				caseSensitive = b
			}
		}

		cmpValues := func(a, b value.Value) int {
			if !caseSensitive {
				if s1, ok := a.AsString(); ok {
					if s2, ok := b.AsString(); ok {
						lowerCmp := strings.Compare(strings.ToLower(s1), strings.ToLower(s2))
						if lowerCmp != 0 {
							return lowerCmp
						}
						return strings.Compare(s1, s2)
					}
				}
			}
			if cmp, ok := a.Compare(b); ok {
				return cmp
			}
			return strings.Compare(a.Repr(), b.Repr())
		}

		if byValue {
			sort.Slice(keys, func(i, j int) bool {
				cmp := cmpValues(m[keys[i]], m[keys[j]])
				if reverse {
					return cmp > 0
				}
				return cmp < 0
			})
		} else {
			sort.Slice(keys, func(i, j int) bool {
				cmp := cmpValues(value.FromString(keys[i]), value.FromString(keys[j]))
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
				m[k],
			}))
		}
		return value.FromSlice(result), nil
	}
	return value.FromSlice(nil), nil
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
func FilterAttr(_ State, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
	if err := maxArgs(args, kwargs, 1); err != nil {
		return value.Undefined(), err
	}
	if len(args) < 1 {
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrMissingArgument, "missing argument")
	}
	name, _ := args[0].AsString()
	return val.GetAttr(name), nil
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
func FilterIndent(_ State, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
	s, ok := val.AsString()
	if !ok {
		return val, nil
	}

	if len(kwargs) > 0 {
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrTooManyArguments, "too many keyword arguments")
	}

	width := 4
	if len(args) > 0 {
		if w, ok := args[0].AsInt(); ok {
			width = int(w)
		}
	}

	first := false
	if len(args) > 1 {
		if b, ok := args[1].AsBool(); ok {
			first = b
		}
	}

	blank := false
	if len(args) > 2 {
		if b, ok := args[2].AsBool(); ok {
			blank = b
		}
	}

	indent := strings.Repeat(" ", width)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if i == 0 && !first {
			continue
		}
		if line == "" && !blank {
			continue
		}
		lines[i] = indent + line
	}
	return value.FromString(strings.Join(lines, "\n")), nil
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
func FilterPprint(_ State, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
	if err := noArgs(args, kwargs); err != nil {
		return value.Undefined(), err
	}
	return value.FromString(pprintValue(val, 0)), nil
}

func pprintValue(val value.Value, indent int) string {
	pad := strings.Repeat(" ", indent)
	switch val.Kind() {
	case value.KindSeq:
		items, _ := val.AsSlice()
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
		m, _ := val.AsMap()
		if len(m) == 0 {
			return "{}"
		}
		// The mapping's own order, so a pretty-printed map does not disagree
		// with the same map rendered normally.
		keys, _ := val.MapKeys()
		var sb strings.Builder
		sb.WriteString("{\n")
		for _, k := range keys {
			sb.WriteString(strings.Repeat(" ", indent+4))
			sb.WriteString(fmt.Sprintf("%q: %s,", k, pprintValue(m[k], indent+4)))
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
func FilterTojson(_ State, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
	// `tojson(value, indent: Option<Value>, kwargs)`: `true` means two spaces,
	// `false` means compact, and a number is a literal width
	// (filters.rs:1007-1020).
	indentArg := value.Undefined()
	switch {
	case len(args) > 1:
		return value.Undefined(), mjerrors.NewError(mjerrors.ErrTooManyArguments, "received too many arguments")
	case len(args) == 1:
		indentArg = args[0]
	}
	if kw, ok := kwargs["indent"]; ok {
		if len(args) > 0 {
			return value.Undefined(), mjerrors.NewError(mjerrors.ErrTooManyArguments, "received too many arguments")
		}
		indentArg = kw
	}
	for name := range kwargs {
		if name != "indent" {
			return value.Undefined(), mjerrors.NewError(mjerrors.ErrTooManyArguments,
				fmt.Sprintf("unknown keyword argument '%s'", name))
		}
	}

	indent := -1
	if !indentArg.IsUndefined() && !indentArg.IsNone() {
		if indentArg.Kind() == value.KindBool {
			if b, _ := indentArg.AsBool(); b {
				indent = 2
			}
		} else {
			n, ok := indentArg.AsInt()
			if !ok || !indentArg.IsActualInt() || n < 0 {
				return value.Undefined(), mjerrors.NewError(mjerrors.ErrInvalidOperation,
					fmt.Sprintf("cannot convert %s to usize", indentArg.Kind()))
			}
			indent = int(n)
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
func FilterUrlencode(_ State, val value.Value, _ []value.Value, _ map[string]value.Value) (value.Value, error) {
	// Check if it's a map (dict) - encode as query string
	if m, ok := val.AsMap(); ok {
		var parts []string
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := m[k]
			if v.IsNone() {
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
