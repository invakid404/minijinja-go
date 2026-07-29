// Package tests provides MiniJinja's built-in tests.
package tests

import (
	"math"
	"math/big"
	"strings"

	"github.com/invakid404/minijinja-go/v2/filters"
	mjerrors "github.com/invakid404/minijinja-go/v2/internal/errors"
	"github.com/invakid404/minijinja-go/v2/internal/unicodecase"
	"github.com/invakid404/minijinja-go/v2/value"
)

// noArgs and oneArg are the tests' side of the engine's `from_args` contract.
// A test's Rust signature fixes its arity exactly, and keyword arguments reach
// it as one trailing value, so `1 is defined(2)` and `1 is eq(1, 2)` are
// argument errors rather than ignored extras.
// strSubject is the `&str` ArgType: a value that is not a string is a
// conversion error, not a false answer (value/argtypes.rs:469-480). `42 is
// lower` fails on both sides.
func strSubject(val value.Value) (string, error) {
	s, ok := val.AsString()
	if !ok {
		return "", mjerrors.NewError(mjerrors.ErrInvalidOperation, "value is not a string")
	}
	return s, nil
}

// coerceStr is the `Cow<'_, str>` ArgType: a string is taken as is and
// anything else is stringified (value/argtypes.rs:547-568).
func coerceStr(val value.Value) string {
	if s, ok := val.AsString(); ok {
		return s
	}
	return val.String()
}

func noArgs(args []value.Value) error {
	if len(args) > 0 {
		return mjerrors.NewError(mjerrors.ErrTooManyArguments, "too many arguments")
	}
	return nil
}

func oneArg(args []value.Value) error {
	if len(args) < 1 {
		return mjerrors.NewError(mjerrors.ErrMissingArgument, "missing argument")
	}
	if len(args) > 1 {
		return mjerrors.NewError(mjerrors.ErrTooManyArguments, "too many arguments")
	}
	return nil
}

type undefinedBehaviorProvider interface {
	UndefinedBehavior() value.UndefinedBehavior
}

func undefinedBehavior(state filters.State) value.UndefinedBehavior {
	if state == nil {
		return value.UndefinedLenient
	}
	if provider, ok := state.(undefinedBehaviorProvider); ok {
		return provider.UndefinedBehavior()
	}
	return value.UndefinedLenient
}

// TestDefined checks if a value is defined.
//
// Returns true if the value is not undefined.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddTest("defined", TestDefined)
//
// Template usage:
//
//	{% if my_variable is defined %}
//	  {{ my_variable }}
//	{% endif %}
func TestDefined(_ filters.State, val value.Value, args []value.Value) (bool, error) {
	if err := noArgs(args); err != nil {
		return false, err
	}
	return !val.IsUndefined(), nil
}

// TestUndefined checks if a value is undefined.
//
// Returns true if the value is undefined.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddTest("undefined", TestUndefined)
//
// Template usage:
//
//	{% if my_variable is undefined %}
//	  Variable not set
//	{% endif %}
func TestUndefined(_ filters.State, val value.Value, args []value.Value) (bool, error) {
	if err := noArgs(args); err != nil {
		return false, err
	}
	return val.IsUndefined(), nil
}

// TestNone checks if a value is none/null.
//
// Returns true if the value is none.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddTest("none", TestNone)
//
// Template usage:
//
//	{% if value is none %}
//	  Value is null
//	{% endif %}
func TestNone(_ filters.State, val value.Value, args []value.Value) (bool, error) {
	if err := noArgs(args); err != nil {
		return false, err
	}
	return val.IsNone(), nil
}

// TestTrue checks if a value is the boolean true.
//
// This is a strict check for the boolean value true, not truthiness.
// Use value.IsTrue() for truthiness checks.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddTest("true", TestTrue)
//
// Template usage:
//
//	{% if value is true %}
//	  Value is exactly true
//	{% endif %}
func TestTrue(_ filters.State, val value.Value, args []value.Value) (bool, error) {
	if err := noArgs(args); err != nil {
		return false, err
	}
	if b, ok := val.AsBool(); ok {
		return b, nil
	}
	return false, nil
}

// TestFalse checks if a value is the boolean false.
//
// This is a strict check for the boolean value false, not falsiness.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddTest("false", TestFalse)
//
// Template usage:
//
//	{% if value is false %}
//	  Value is exactly false
//	{% endif %}
func TestFalse(_ filters.State, val value.Value, args []value.Value) (bool, error) {
	if err := noArgs(args); err != nil {
		return false, err
	}
	if b, ok := val.AsBool(); ok {
		return !b, nil
	}
	return false, nil
}

// TestOdd checks if a number is odd.
//
// Returns true if the value is an odd integer.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddTest("odd", TestOdd)
//
// Template usage:
//
//	{% if loop.index is odd %}
//	  <div class="odd">{{ item }}</div>
//	{% endif %}
//
//	{{ 41 is odd }}
//	  -> true
//	{{ 42 is odd }}
//	  -> false
func TestOdd(_ filters.State, val value.Value, args []value.Value) (bool, error) {
	if err := noArgs(args); err != nil {
		return false, err
	}
	if len(args) > 0 {
		return false, mjerrors.NewError(mjerrors.ErrInvalidOperation, "odd test expects no arguments")
	}
	// `tests::is_odd` is `i128::try_from(v).ok().is_some_and(|x| x % 2 != 0)`
	// (tests.rs:133-135), so the conversion is i128 rather than i64 and an
	// integer past int64 still decides.
	if i, ok := val.AsBigInt(); ok {
		return i.Bit(0) == 1, nil
	}
	return false, nil
}

// TestEven checks if a number is even.
//
// Returns true if the value is an even integer.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddTest("even", TestEven)
//
// Template usage:
//
//	{% for item in items %}
//	  <li class="{{ 'even' if loop.index is even else 'odd' }}">
//	    {{ item }}
//	  </li>
//	{% endfor %}
//
//	{{ 42 is even }}
//	  -> true
//	{{ 41 is even }}
//	  -> false
func TestEven(_ filters.State, val value.Value, args []value.Value) (bool, error) {
	if err := noArgs(args); err != nil {
		return false, err
	}
	if len(args) > 0 {
		return false, mjerrors.NewError(mjerrors.ErrInvalidOperation, "even test expects no arguments")
	}
	// `tests::is_even` (tests.rs:143-145); see TestOdd for the conversion.
	if i, ok := val.AsBigInt(); ok {
		return i.Bit(0) == 0, nil
	}
	return false, nil
}

// TestDivisibleBy checks if a value is divisible by another number.
//
// Returns true if the value is evenly divisible by the given number.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddTest("divisibleby", TestDivisibleBy)
//
// Template usage:
//
//	{% if count is divisibleby(3) %}
//	  Count is a multiple of 3
//	{% endif %}
//
//	{{ 42 is divisibleby(2) }}
//	  -> true
//	{{ 42 is divisibleby(5) }}
//	  -> false
func TestDivisibleBy(_ filters.State, val value.Value, args []value.Value) (bool, error) {
	if err := oneArg(args); err != nil {
		return false, err
	}
	if len(args) < 1 {
		return false, mjerrors.NewError(mjerrors.ErrMissingArgument, "missing argument")
	}
	// `coerce(v, other)` gives an I128 pair for two integers, so a value past
	// int64 decides here rather than being truncated to zero: the port ignored
	// AsInt's success flag and `18446744073709551616 is divisibleby(3)` came
	// out true. AsBigInt is the engine's own i128 conversion.
	if lhs, lok := val.AsBigInt(); lok {
		if rhs, rok := args[0].AsBigInt(); rok {
			if rhs.Sign() == 0 {
				// The engine computes `a % b` on integers and panics on a zero
				// divisor. A panic is not a behaviour worth reproducing, and
				// answering `false` where the reference implementation aborts
				// is the dangerous direction, so this refuses instead. Corpus:
				// test/divisibleby-zero.
				return false, mjerrors.NewError(mjerrors.ErrInvalidOperation,
					"cannot check divisibility by zero")
			}
			return new(big.Int).Rem(lhs, rhs).Sign() == 0, nil
		}
	}
	// Floats coerce and follow IEEE semantics, where a zero divisor is a NaN
	// remainder rather than a trap: `6.0 is divisibleby(0.0)` is false.
	if a, ok := val.AsFloat(); ok {
		if b, ok := args[0].AsFloat(); ok {
			return math.Mod(a, b) == 0, nil
		}
	}
	return false, nil
}

// TestEq checks if two values are equal.
//
// This is the test version of the == operator. It's useful when combined
// with filters like select/reject.
//
// This test is also registered under the aliases "equalto" and "==".
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddTest("eq", TestEq)
//
// Template usage:
//
//	{{ 1 is eq(1) }}
//	  -> true
//	{{ [1, 2, 3]|select("==", 1) }}
//	  -> [1]
func TestEq(_ filters.State, val value.Value, args []value.Value) (bool, error) {
	if err := oneArg(args); err != nil {
		return false, err
	}
	if len(args) < 1 {
		return false, nil
	}
	return val.Equal(args[0]), nil
}

// TestNe checks if two values are not equal.
//
// This is the test version of the != operator. It's useful when combined
// with filters like select/reject.
//
// This test is also registered under the alias "!=".
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddTest("ne", TestNe)
//
// Template usage:
//
//	{{ 2 is ne(1) }}
//	  -> true
//	{{ [1, 2, 3]|select("!=", 1) }}
//	  -> [2, 3]
func TestNe(_ filters.State, val value.Value, args []value.Value) (bool, error) {
	if err := oneArg(args); err != nil {
		return false, err
	}
	if len(args) < 1 {
		return false, nil
	}
	return !val.Equal(args[0]), nil
}

// TestLt checks if a value is less than another.
//
// This is the test version of the < operator. It's useful when combined
// with filters like select/reject.
//
// This test is also registered under the aliases "lessthan" and "<".
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddTest("lt", TestLt)
//
// Template usage:
//
//	{{ 1 is lt(2) }}
//	  -> true
//	{{ [1, 2, 3]|select("<", 2) }}
//	  -> [1]
func TestLt(_ filters.State, val value.Value, args []value.Value) (bool, error) {
	if err := oneArg(args); err != nil {
		return false, err
	}
	if len(args) < 1 {
		return false, nil
	}
	if cmp, ok := val.Compare(args[0]); ok {
		return cmp < 0, nil
	}
	return false, nil
}

// TestLe checks if a value is less than or equal to another.
//
// This is the test version of the <= operator. It's useful when combined
// with filters like select/reject.
//
// This test is also registered under the alias "<=".
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddTest("le", TestLe)
//
// Template usage:
//
//	{{ 1 is le(2) }}
//	  -> true
//	{{ [1, 2, 3]|select("<=", 2) }}
//	  -> [1, 2]
func TestLe(_ filters.State, val value.Value, args []value.Value) (bool, error) {
	if err := oneArg(args); err != nil {
		return false, err
	}
	if len(args) < 1 {
		return false, nil
	}
	if cmp, ok := val.Compare(args[0]); ok {
		return cmp <= 0, nil
	}
	return false, nil
}

// TestGt checks if a value is greater than another.
//
// This is the test version of the > operator. It's useful when combined
// with filters like select/reject.
//
// This test is also registered under the aliases "greaterthan" and ">".
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddTest("gt", TestGt)
//
// Template usage:
//
//	{{ 2 is gt(1) }}
//	  -> true
//	{{ [1, 2, 3]|select(">", 2) }}
//	  -> [3]
func TestGt(_ filters.State, val value.Value, args []value.Value) (bool, error) {
	if err := oneArg(args); err != nil {
		return false, err
	}
	if len(args) < 1 {
		return false, nil
	}
	if cmp, ok := val.Compare(args[0]); ok {
		return cmp > 0, nil
	}
	return false, nil
}

// TestGe checks if a value is greater than or equal to another.
//
// This is the test version of the >= operator. It's useful when combined
// with filters like select/reject.
//
// This test is also registered under the alias ">=".
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddTest("ge", TestGe)
//
// Template usage:
//
//	{{ 2 is ge(1) }}
//	  -> true
//	{{ [1, 2, 3]|select(">=", 2) }}
//	  -> [2, 3]
func TestGe(_ filters.State, val value.Value, args []value.Value) (bool, error) {
	if err := oneArg(args); err != nil {
		return false, err
	}
	if len(args) < 1 {
		return false, nil
	}
	if cmp, ok := val.Compare(args[0]); ok {
		return cmp >= 0, nil
	}
	return false, nil
}

// TestIn checks if a value is contained in a sequence.
//
// This is the test version of the "in" operator. It's useful when combined
// with filters like select/reject.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddTest("in", TestIn)
//
// Template usage:
//
//	{{ 1 is in([1, 2, 3]) }}
//	  -> true
//	{{ [1, 2, 3]|select("in", [1, 2]) }}
//	  -> [1, 2]
func TestIn(state filters.State, val value.Value, args []value.Value) (bool, error) {
	if err := oneArg(args); err != nil {
		return false, err
	}
	if len(args) < 1 {
		return false, nil
	}
	container := args[0]
	behavior := undefinedBehavior(state)
	if container.IsUndefined() && (behavior == value.UndefinedStrict || behavior == value.UndefinedSemiStrict) && !container.IsSilentUndefined() {
		return false, mjerrors.NewError(mjerrors.ErrUndefinedVar, "undefined value")
	}
	return container.Contains(val), nil
}

// TestString checks if a value is a string.
//
// Returns true if the value's kind is string.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddTest("string", TestString)
//
// Template usage:
//
//	{{ "42" is string }}
//	  -> true
//	{{ 42 is string }}
//	  -> false
func TestString(_ filters.State, val value.Value, args []value.Value) (bool, error) {
	if err := noArgs(args); err != nil {
		return false, err
	}
	return val.Kind() == value.KindString, nil
}

// TestNumber checks if a value is a number.
//
// Returns true if the value is a number (either integer or float).
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddTest("number", TestNumber)
//
// Template usage:
//
//	{{ 42 is number }}
//	  -> true
//	{{ "42" is number }}
//	  -> false
func TestNumber(_ filters.State, val value.Value, args []value.Value) (bool, error) {
	if err := noArgs(args); err != nil {
		return false, err
	}
	return val.Kind() == value.KindNumber, nil
}

// TestInteger checks if a value is an integer.
//
// Returns true if the value is an actual integer (not a float).
// This test is also registered under the alias "int".
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddTest("integer", TestInteger)
//
// Template usage:
//
//	{{ 42 is integer }}
//	  -> true
//	{{ 42.0 is integer }}
//	  -> false
func TestInteger(_ filters.State, val value.Value, args []value.Value) (bool, error) {
	if err := noArgs(args); err != nil {
		return false, err
	}
	// `tests::is_integer` is `v.is_integer()` (tests.rs:179-181), a question
	// about the PAYLOAD: every integer ValueRepr answers true and nothing else
	// does. Asking it through a conversion instead made `true is integer` and
	// an integer past int64 answer wrongly in opposite directions.
	return val.IsActualInt(), nil
}

// TestFloat checks if a value is a float.
//
// Returns true if the value is a floating-point number.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddTest("float", TestFloat)
//
// Template usage:
//
//	{{ 42.0 is float }}
//	  -> true
//	{{ 42 is float }}
//	  -> false
func TestFloat(_ filters.State, val value.Value, args []value.Value) (bool, error) {
	if err := noArgs(args); err != nil {
		return false, err
	}
	return val.IsActualFloat(), nil
}

// TestBoolean checks if a value is a boolean.
//
// Returns true if the value is a boolean (true or false).
// This test is also registered under the alias "bool".
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddTest("boolean", TestBoolean)
//
// Template usage:
//
//	{{ true is boolean }}
//	  -> true
//	{{ 1 is boolean }}
//	  -> false
func TestBoolean(_ filters.State, val value.Value, args []value.Value) (bool, error) {
	if err := noArgs(args); err != nil {
		return false, err
	}
	return val.Kind() == value.KindBool, nil
}

// TestSafe checks if a value is marked as safe.
//
// Returns true if the value has been marked as safe for auto-escaping.
// This test is also registered under the alias "escaped".
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddTest("safe", TestSafe)
//
// Template usage:
//
//	{{ "<hello>"|escape is safe }}
//	  -> true
func TestSafe(_ filters.State, val value.Value, args []value.Value) (bool, error) {
	if err := noArgs(args); err != nil {
		return false, err
	}
	return val.IsSafe(), nil
}

// TestSameAs checks if two values are the exact same object.
//
// This is a stricter comparison than equality. Values that have the same
// structure but are different objects will not compare as "same".
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddTest("sameas", TestSameAs)
//
// Template usage:
//
//	{{ [1, 2, 3] is sameas([1, 2, 3]) }}
//	  -> false
//	{{ false is sameas(false) }}
//	  -> true
func TestSameAs(_ filters.State, val value.Value, args []value.Value) (bool, error) {
	if err := oneArg(args); err != nil {
		return false, err
	}
	if len(args) < 1 {
		return false, nil
	}
	return val.SameAs(args[0]), nil
}

// TestLower checks if a string is all lowercase.
//
// Returns true if all alphabetic characters in the string are lowercase.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddTest("lower", TestLower)
//
// Template usage:
//
//	{{ "foo" is lower }}
//	  -> true
//	{{ "Foo" is lower }}
//	  -> false
func TestLower(_ filters.State, val value.Value, args []value.Value) (bool, error) {
	if err := noArgs(args); err != nil {
		return false, err
	}
	s, err := strSubject(val)
	if err != nil {
		return false, err
	}
	// `name.chars().all(char::is_lowercase)` (tests.rs:...): EVERY character
	// must be lowercase, so a digit or a punctuation mark makes it false —
	// the port skipped non-letters and answered true for "a1".
	for _, r := range s {
		if !unicodecase.IsLower(r) {
			return false, nil
		}
	}
	return true, nil
}

// TestUpper checks if a string is all uppercase.
//
// Returns true if all alphabetic characters in the string are uppercase.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddTest("upper", TestUpper)
//
// Template usage:
//
//	{{ "FOO" is upper }}
//	  -> true
//	{{ "Foo" is upper }}
//	  -> false
func TestUpper(_ filters.State, val value.Value, args []value.Value) (bool, error) {
	if err := noArgs(args); err != nil {
		return false, err
	}
	s, err := strSubject(val)
	if err != nil {
		return false, err
	}
	for _, r := range s {
		if !unicodecase.IsUpper(r) {
			return false, nil
		}
	}
	return true, nil
}

// TestFilter checks if a filter with the given name exists.
//
// This is useful for checking whether certain template features are available.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddTest("filter", TestFilter)
//
// Template usage:
//
//	{% if "tojson" is filter %}
//	  JSON serialization available
//	{% endif %}
func TestFilter(state filters.State, val value.Value, args []value.Value) (bool, error) {
	if err := noArgs(args); err != nil {
		return false, err
	}
	name, err := strSubject(val)
	if err != nil {
		return false, err
	}
	_, exists := state.GetFilter(name)
	return exists, nil
}

// TestTest checks if a test with the given name exists.
//
// This is useful for checking whether certain template features are available.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddTest("test", TestTest)
//
// Template usage:
//
//	{% if "greaterthan" is test %}
//	  Comparison tests available
//	{% endif %}
func TestTest(state filters.State, val value.Value, args []value.Value) (bool, error) {
	if err := noArgs(args); err != nil {
		return false, err
	}
	name, err := strSubject(val)
	if err != nil {
		return false, err
	}
	_, exists := state.GetTest(name)
	return exists, nil
}

// TestSequence checks if a value is a sequence.
//
// Returns true if the value is a list/array.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddTest("sequence", TestSequence)
//
// Template usage:
//
//	{{ [1, 2, 3] is sequence }}
//	  -> true
//	{{ 42 is sequence }}
//	  -> false
func TestSequence(_ filters.State, val value.Value, args []value.Value) (bool, error) {
	if err := noArgs(args); err != nil {
		return false, err
	}
	return val.Kind() == value.KindSeq, nil
}

// TestMapping checks if a value is a mapping/dict.
//
// Returns true if the value is a map/dictionary.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddTest("mapping", TestMapping)
//
// Template usage:
//
//	{{ {"foo": "bar"} is mapping }}
//	  -> true
//	{{ [1, 2, 3] is mapping }}
//	  -> false
func TestMapping(_ filters.State, val value.Value, args []value.Value) (bool, error) {
	if err := noArgs(args); err != nil {
		return false, err
	}
	return val.Kind() == value.KindMap, nil
}

// TestIterable checks if a value can be iterated over.
//
// Returns true if the value supports iteration (sequences, maps, strings, etc.).
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddTest("iterable", TestIterable)
//
// Template usage:
//
//	{{ [1, 2, 3] is iterable }}
//	  -> true
//	{{ 42 is iterable }}
//	  -> false
func TestIterable(_ filters.State, val value.Value, args []value.Value) (bool, error) {
	if err := noArgs(args); err != nil {
		return false, err
	}
	// `v.try_iter().is_ok()` (tests.rs:223-225): none and undefined iterate as
	// empty and are therefore iterable, while a number is not.
	switch val.Kind() {
	case value.KindSeq, value.KindMap, value.KindIterable, value.KindString,
		value.KindNone, value.KindUndefined:
		return true, nil
	default:
		return val.Iter() != nil, nil
	}
}

// TestStartingWith checks if a string starts with a given prefix.
//
// Returns true if the string starts with the specified prefix.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddTest("startingwith", TestStartingWith)
//
// Template usage:
//
//	{{ "foobar" is startingwith("foo") }}
//	  -> true
//	{{ "foobar" is startingwith("bar") }}
//	  -> false
func TestStartingWith(_ filters.State, val value.Value, args []value.Value) (bool, error) {
	if err := oneArg(args); err != nil {
		return false, err
	}
	if len(args) < 1 {
		return false, nil
	}
	// Both sides are `Cow<'_, str>`, which stringifies a non-string value
	// rather than refusing it: `42 is startingwith(4)` is true.
	return strings.HasPrefix(coerceStr(val), coerceStr(args[0])), nil
}

// TestEndingWith checks if a string ends with a given suffix.
//
// Returns true if the string ends with the specified suffix.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddTest("endingwith", TestEndingWith)
//
// Template usage:
//
//	{{ "foobar" is endingwith("bar") }}
//	  -> true
//	{{ "foobar" is endingwith("foo") }}
//	  -> false
func TestEndingWith(_ filters.State, val value.Value, args []value.Value) (bool, error) {
	if err := oneArg(args); err != nil {
		return false, err
	}
	if len(args) < 1 {
		return false, nil
	}
	return strings.HasSuffix(coerceStr(val), coerceStr(args[0])), nil
}

// TestContaining checks if a value contains another value.
//
// For strings, this checks if the substring is present. For sequences and
// maps, it checks if the item or key is present.
//
// Example:
//
//	env := minijinja.NewEnvironment()
//	env.AddTest("containing", TestContaining)
//
// Template usage:
//
//	{{ "foobar" is containing("oob") }}
//	  -> true
//	{{ [1, 2, 3] is containing(2) }}
//	  -> true
func TestContaining(_ filters.State, val value.Value, args []value.Value) (bool, error) {
	if err := oneArg(args); err != nil {
		return false, err
	}
	if len(args) < 1 {
		return false, nil
	}
	return val.Contains(args[0]), nil
}
