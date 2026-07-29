package filters

import (
	"fmt"
	"math"

	mjerrors "github.com/invakid404/minijinja-go/v2/internal/errors"
	"github.com/invakid404/minijinja-go/v2/value"
)

// Args is the engine's `from_args` argument contract, which is stricter and
// more uniform than "read what you need and ignore the rest".
//
// In the engine a call's keyword arguments are not a separate channel: they
// arrive as ONE trailing `Kwargs` value in the positional slice
// (value/argtypes.rs:190-240). Three consequences follow, and all three are
// observable:
//
//   - a filter whose signature has no `Kwargs` parameter sees a keyword
//     argument as one extra positional value, so `[1]|sort(foo=1)` and
//     `'x'|upper(a=1)` are argument errors rather than ignored;
//   - if a parameter slot is still open, that trailing value lands in it, and
//     the error is whatever that parameter's type says: a string parameter
//     reports "cannot convert kwargs to string" (an invalid operation), while
//     a `Value` parameter accepts it as a plain map — which is exactly how
//     `namespace(count=0)` and `dict(a=1)` work. The two spellings are the
//     engine's, not a slip: a string parameter names the value `kwargs` and a
//     primitive one names it `map`, and `messages_test.go` compares both
//     against recorded engine output;
//   - a filter that does declare `Kwargs` takes that trailing value FIRST,
//     before its positional parameters are filled (argtypes.rs:199-210), so
//     `|groupby(attribute="city")` leaves the positional attribute slot empty
//     instead of putting a map in it — and it must then consume every keyword
//     it was given, so an unknown one is "unknown keyword argument" rather
//     than silently dropped.
//
// Call Kwargs() before reading positional arguments whenever the Rust
// signature has a Kwargs parameter; that ordering is the contract.
//
// Answering a call the engine rejects is the dangerous direction, so every
// builtin routes its arguments through this type.
//
// Keyword arguments arrive as an ORDERED mapping, because the engine's are one:
// `dict(b=1, a=2)` builds `{"b": 1, "a": 2}`, and the keyword an argument error
// names is the first unused one in that same order rather than whichever a Go
// map happened to yield first.
type Args struct {
	positional []value.Value
	kwargs     *value.OrderedMap

	next        int
	kwargsSlot  bool // the trailing Kwargs value is still unconsumed
	kwargsTaken bool // consumed by Kwargs(), so keyword names get validated
	used        map[string]bool
}

// NewArgs models a call as the engine sees it.
func NewArgs(args []value.Value, kwargs *value.OrderedMap) *Args {
	return &Args{
		positional: args,
		kwargs:     kwargs,
		kwargsSlot: kwargs.Len() > 0,
		used:       make(map[string]bool, kwargs.Len()),
	}
}

func tooMany() error {
	return mjerrors.NewError(mjerrors.ErrTooManyArguments, "too many arguments")
}

func missing() error {
	return mjerrors.NewError(mjerrors.ErrMissingArgument, "missing argument")
}

func invalidOp(format string, a ...any) error {
	return mjerrors.NewError(mjerrors.ErrInvalidOperation, fmt.Sprintf(format, a...))
}

// remaining reports how many values are still unconsumed, counting the
// trailing keyword-argument value as one.
func (a *Args) remaining() int {
	n := len(a.positional) - a.next
	if a.kwargsSlot {
		n++
	}
	return n
}

// peek returns the next value and whether it is the trailing Kwargs value.
func (a *Args) peek() (value.Value, bool, bool) {
	if a.next < len(a.positional) {
		return a.positional[a.next], false, true
	}
	if a.kwargsSlot {
		return value.FromOrderedMap(a.kwargs), true, true
	}
	return value.Undefined(), false, false
}

func (a *Args) advance(isKwargs bool) {
	if isKwargs {
		a.kwargsSlot = false
		return
	}
	a.next++
}

// Value takes the next argument as an arbitrary value. A keyword-argument
// value is accepted here, as a map, which is what makes `namespace(x=1)` work.
func (a *Args) Value() (value.Value, error) {
	v, isKwargs, ok := a.peek()
	if !ok {
		return value.Undefined(), missing()
	}
	a.advance(isKwargs)
	return v, nil
}

// OptValue takes an optional argument. Absent, none and undefined all mean
// "not given" (value/argtypes.rs:522-545).
func (a *Args) OptValue() (value.Value, bool, error) {
	v, isKwargs, ok := a.peek()
	if !ok {
		return value.Undefined(), false, nil
	}
	a.advance(isKwargs)
	if v.IsNone() || v.IsUndefined() {
		return value.Undefined(), false, nil
	}
	return v, true, nil
}

// Str takes a required string argument. A value that is not a string is an
// error rather than being stringified; this is the `&str`/`Arc<str>` contract.
func (a *Args) Str() (string, error) {
	v, isKwargs, ok := a.peek()
	if !ok {
		return "", missing()
	}
	a.advance(isKwargs)
	if isKwargs {
		return "", invalidOp("cannot convert kwargs to string")
	}
	s, isStr := v.AsString()
	if !isStr {
		return "", invalidOp("value is not a string")
	}
	return s, nil
}

// OptStr takes an optional string argument.
func (a *Args) OptStr() (string, bool, error) {
	v, isKwargs, ok := a.peek()
	if !ok {
		return "", false, nil
	}
	a.advance(isKwargs)
	if v.IsNone() || v.IsUndefined() {
		return "", false, nil
	}
	if isKwargs {
		return "", false, invalidOp("cannot convert kwargs to string")
	}
	s, isStr := v.AsString()
	if !isStr {
		return "", false, invalidOp("value is not a string")
	}
	return s, true, nil
}

// CoerceStr takes a required argument as the `Cow<'_, str>` type does: a
// string is taken as is and anything else is stringified, but a keyword
// argument is still rejected (value/argtypes.rs:547-568).
func (a *Args) CoerceStr() (string, error) {
	v, isKwargs, ok := a.peek()
	if !ok {
		return "", missing()
	}
	a.advance(isKwargs)
	if isKwargs {
		return "", invalidOp("cannot convert kwargs to string")
	}
	if s, isStr := v.AsString(); isStr {
		return s, nil
	}
	return v.String(), nil
}

// OptCoerceStr is the optional form of CoerceStr.
func (a *Args) OptCoerceStr() (string, bool, error) {
	v, isKwargs, ok := a.peek()
	if !ok {
		return "", false, nil
	}
	a.advance(isKwargs)
	if v.IsNone() || v.IsUndefined() {
		return "", false, nil
	}
	if isKwargs {
		return "", false, invalidOp("cannot convert kwargs to string")
	}
	if s, isStr := v.AsString(); isStr {
		return s, true, nil
	}
	return v.String(), true, nil
}

// Int takes a required integer argument, named for the Rust type it models so
// the error text matches ("cannot convert X to usize").
func (a *Args) Int(rustType string) (int64, error) {
	n, ok, err := a.OptIntPresent(rustType)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, missing()
	}
	return n, nil
}

// OptInt takes an optional integer argument.
func (a *Args) OptInt(rustType string) (int64, bool, error) {
	v, isKwargs, ok := a.peek()
	if !ok {
		return 0, false, nil
	}
	if !isKwargs && (v.IsNone() || v.IsUndefined()) {
		a.advance(isKwargs)
		return 0, false, nil
	}
	n, err := a.intValue(rustType)
	if err != nil {
		return 0, false, err
	}
	return n, true, nil
}

// OptIntPresent is OptInt without the none/undefined shortcut, for a required
// parameter whose absence is a missing argument.
func (a *Args) OptIntPresent(rustType string) (int64, bool, error) {
	if _, _, ok := a.peek(); !ok {
		return 0, false, nil
	}
	n, err := a.intValue(rustType)
	if err != nil {
		return 0, false, err
	}
	return n, true, nil
}

func (a *Args) intValue(rustType string) (int64, error) {
	v, isKwargs, _ := a.peek()
	a.advance(isKwargs)
	if isKwargs {
		return 0, invalidOp("cannot convert map to %s", rustType)
	}
	// The engine's integer ArgTypes go through one conversion
	// (value/argtypes.rs:410-435), which is what `Value.AsInt` models since the
	// numeric slice: a bool converts, an INTEGRAL float converts, a fractional
	// or out-of-range one does not. `range(3.0)` is `[0, 1, 2]` and `range(1.5)`
	// is an error, on both sides.
	return ConvertInt(v, rustType)
}

// ConvertInt is the engine's integer ArgType conversion, CHECKED against the
// width the parameter actually declares.
//
// The coercion is [value.Value.AsInt] — `TryFrom<Value> for i64`, where a bool
// and an integral float convert and a fractional one does not. The RANGE is
// the target's, and the fork used to ignore it: `1.5|round(2147483648)` is an
// invalid operation because `round`'s precision is an `i32`.
//
// rustType is the type the parameter declares ("i32", "i64", "isize"); it
// bounds the value and names the error. `usize` is NOT one of them: its range
// does not fit an int64 on a 64-bit target, so it has its own conversion —
// see [ConvertUsize].
func ConvertInt(v value.Value, rustType string) (int64, error) {
	n, ok := v.AsInt()
	if !ok {
		return 0, invalidOp("cannot convert %s to %s", v.Kind(), rustType)
	}
	if rustType == "i32" && (n < math.MinInt32 || n > math.MaxInt32) {
		return 0, invalidOp("cannot convert %s to %s", v.Kind(), rustType)
	}
	return n, nil
}

// Usize takes a required `usize` argument at the width that type really has.
//
// It is separate from [Args.Int] because it cannot share its return type: a
// `usize` on a 64-bit target accepts the whole upper half of `u64`, which does
// not fit an int64. Routing it through [value.Value.AsInt] made the fork refuse
// a range the engine accepts — `[]|batch(9223372036854775808)` is an
// invalid_operation here and a converted argument there.
func (a *Args) Usize() (uint64, error) {
	v, isKwargs, ok := a.peek()
	if !ok {
		return 0, missing()
	}
	a.advance(isKwargs)
	if isKwargs {
		return 0, invalidOp("cannot convert map to usize")
	}
	return ConvertUsize(v)
}

// ConvertUsize is the engine's `usize` ArgType conversion.
func ConvertUsize(v value.Value) (uint64, error) {
	n, ok := v.AsUsize()
	if !ok {
		return 0, invalidOp("cannot convert %s to usize", v.Kind())
	}
	return n, nil
}

// maxAllocatingArg is the largest `usize` argument this fork will let size an
// allocation.
//
// Several builtins turn such an argument straight into memory: `batch` and
// `slice` reserve `count` elements, `indent` and `tojson` build an indentation
// string of `width` spaces. The engine does that unconditionally and ABORTS on
// a value it cannot allocate — `Vec::with_capacity` and `str::repeat` panic
// with "capacity overflow" — so past this bound the two implementations cannot
// both be faithful and safe.
//
// The fork refuses instead, for the same reason it refuses a zero divisor where
// the engine panics (`test/divisibleby-zero`): a library that a caller can make
// abort the process, or exhaust its memory, on ordinary template input is worse
// than one that returns an error. Every such row is a DECLARED divergence in
// the oracle ledger with both signatures pinned, never an unrecorded one.
//
// The bound is deliberately far above any real use — 2^31 elements is a 32 GiB
// slice or a 2 GiB indentation — so it cannot turn a working template into a
// failing one. It is a safety valve, not a semantic limit.
const maxAllocatingArg = 1 << 31

// allocSize checks a converted `usize` against [maxAllocatingArg] and narrows it
// to the Go int the caller allocates with.
func allocSize(n uint64, what string) (int, error) {
	if n > maxAllocatingArg {
		return 0, invalidOp("%s %d is too large to allocate", what, n)
	}
	return int(n), nil
}

// OptBool takes an optional boolean argument.
func (a *Args) OptBool() (bool, error) {
	v, isKwargs, ok := a.peek()
	if !ok {
		return false, nil
	}
	a.advance(isKwargs)
	if v.IsNone() || v.IsUndefined() {
		return false, nil
	}
	if isKwargs {
		return false, invalidOp("cannot convert map to bool")
	}
	if v.Kind() != value.KindBool {
		return false, invalidOp("cannot convert %s to bool", v.Kind())
	}
	b, _ := v.AsBool()
	return b, nil
}

// Rest takes every remaining positional argument, as `Rest<Value>` does. The
// trailing keyword-argument value is part of it, which is how `|map` reads its
// `attribute` keyword.
func (a *Args) Rest() []value.Value {
	rv := append([]value.Value(nil), a.positional[a.next:]...)
	a.next = len(a.positional)
	if a.kwargsSlot {
		rv = append(rv, value.FromOrderedMap(a.kwargs))
		a.kwargsSlot = false
	}
	return rv
}

// Kwargs consumes the trailing keyword-argument value, as a `Kwargs`
// parameter does. Names not read with Get are reported by Done.
func (a *Args) Kwargs() *Args {
	a.kwargsSlot = false
	a.kwargsTaken = true
	return a
}

// KwargsAll is Kwargs for a signature that consumes the whole map rather than
// naming individual keys, as `dict`'s `update_with` does (functions.rs:406).
// There is nothing left over for Done to complain about.
func (a *Args) KwargsAll() *Args {
	a.Kwargs()
	for _, name := range a.kwargs.Keys() {
		a.used[name] = true
	}
	return a
}

// Get reads one keyword argument and marks it used.
func (a *Args) Get(name string) (value.Value, bool) {
	v, ok := a.kwargs.Get(name)
	if ok {
		a.used[name] = true
	}
	return v, ok
}

// GetStr reads a string keyword argument.
func (a *Args) GetStr(name string) (string, bool, error) {
	v, ok := a.Get(name)
	if !ok || v.IsNone() || v.IsUndefined() {
		return "", false, nil
	}
	s, isStr := v.AsString()
	if !isStr {
		return "", false, invalidOp("value is not a string")
	}
	return s, true, nil
}

// GetBool reads a boolean keyword argument.
func (a *Args) GetBool(name string) (bool, bool, error) {
	v, ok := a.Get(name)
	if !ok || v.IsNone() || v.IsUndefined() {
		return false, false, nil
	}
	if v.Kind() != value.KindBool {
		return false, false, invalidOp("cannot convert %s to bool", v.Kind())
	}
	b, _ := v.AsBool()
	return b, true, nil
}

// Done asserts the call is fully consumed: nothing left over, and every
// keyword argument used if the signature declared Kwargs
// (value/argtypes.rs:892-905).
func (a *Args) Done() error {
	if a.remaining() > 0 {
		return tooMany()
	}
	if !a.kwargsTaken {
		return nil
	}
	// Keyword order, which is the engine's: `assert_all_used` walks an
	// insertion-ordered map and reports the FIRST unused name, so which one a
	// call with several unknown keywords names is deterministic
	// (value/argtypes.rs:892-905).
	for _, name := range a.kwargs.Keys() {
		if !a.used[name] {
			return mjerrors.NewError(mjerrors.ErrTooManyArguments,
				fmt.Sprintf("unknown keyword argument '%s'", name))
		}
	}
	return nil
}
