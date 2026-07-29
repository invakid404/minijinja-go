package value

// Exact model of BAML's numeric core.
//
// BAML v0.223 builds boundaryml/minijinja@8cfc770 (branch "value-cmp"), whose
// numeric core lives in minijinja/src/value/ops.rs and
// minijinja/src/value/argtypes.rs. Every reference in this file is to that
// revision.
//
// The engine does ALL integer arithmetic in checked `i128` after coercing both
// operands with `ops::coerce`. The Go port this fork derives from instead
// laundered integer arithmetic through `float64` and cast back with
// `int64(f1 + f2)` (the pre-fork value/ops.go:36-131). That is wrong twice
// over: it collapses distinct integers above 2^53 onto one `float64`, and Go
// leaves `int64(f)` implementation-defined when `f` is out of range, so the
// same expression rendered `9223372036854775807` on darwin/arm64 and
// `-9223372036854775808` on linux/amd64.
//
// This file reproduces the engine's model exactly, in pure Go with no cgo.
//
// # The integer payload model
//
// A value's PAYLOAD type decides the arithmetic, never its source spelling.
// The engine's integer `ValueRepr` variants map onto this fork's payloads as:
//
//	ValueRepr::I64(i64)   -> int64
//	ValueRepr::U64(u64)   -> u64Value, or bigIntValue{repr: reprU64}  past i64::MAX
//	ValueRepr::I128(i128) -> int64, or bigIntValue{repr: reprI128} past i64::MAX
//	ValueRepr::U128(u128) -> bigIntValue{repr: reprU128}
//	ValueRepr::F64(f64)   -> float64
//	ValueRepr::Bool(bool) -> bool     (an integer to every conversion below)
//
// The repr is not decoration. Two of the engine's behaviours depend on which
// variant holds a given number rather than on the number itself:
//
//  1. `coerce` casts a `u128` pair to `i128` with a WRAPPING cast, so a pair of
//     u128 literals past i128::MAX becomes a pair of negative i128 — while the
//     same magnitudes reached through any other pairing refuse outright,
//     because `i128::try_from` fails on them.
//  2. `as_f64`'s lossless round-trip check casts back to the operand's OWN
//     type, and Rust's float-to-integer casts saturate at that type's bounds.
//     `i64::MAX` is 2^63-1 and its f64 image is 2^63; casting that back to i64
//     saturates to i64::MAX and round-trips, while casting it back to u64 gives
//     2^63 and does not. So `9223372036854775807 == 9223372036854775808.0` is
//     FALSE for an integer LITERAL, which the engine holds as U64, and would be
//     true for the same magnitude held as I64.
//
// (2) is why the U64 repr is carried inside int64 range as well, in its own
// payload type. A payload type rather than a flag on Value, so that every
// consumer inspecting the payload has to name it and none can silently treat a
// U64 as an I64; and it costs no more than an int64.
// TestU64PayloadIsInvisibleExceptToLosslessFloatCoercion is the completeness
// check for that: a U64 and an I64 of the same magnitude must be
// indistinguishable through every exported operation except that one.
//
// # Where each repr comes from
//
// Integer literals are lexed as `u64` when they fit and `u128` otherwise
// (compiler/lexer.rs:481-491), and the fork's lexer does the same
// (internal/lexer/lexer.go:1186-1204). Both emit only NON-NEGATIVE integer
// tokens — a leading `-` is unary minus applied afterwards — so every integer
// literal is a U64 or a U128. That is what `evalConst` and `FromBigInt`
// produce. A host value is whatever its Go type says: `FromInt` is I64 and
// [FromUint64] is U64, exactly as `Value::from(i64)` and `Value::from(u64)`.
//
// Arithmetic results go through `int_as_value` (ops.rs:232-238), which narrows
// to `i64` when the value fits and produces `I128` otherwise; that is
// `fromExactInt` below, and it never produces a U64 or a U128.

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
)

// u64Value is ValueRepr::U64 for a magnitude that also fits int64 — every
// integer literal up to i64::MAX, and any host value of an unsigned Go type.
//
// It is an int64 rather than a uint64 because it only ever holds a value in
// [0, i64::MAX]: a larger u64 is bigIntValue{repr: reprU64}, which is where the
// wide arithmetic already lives.
type u64Value int64

// smallInt returns the int64 that an I64 or a U64 payload carries. The two
// reprs are interchangeable everywhere except `asF64`'s lossless round trip, so
// every other consumer asks this question rather than the payload's type.
func smallInt(v Value) (int64, bool) {
	switch d := v.data.(type) {
	case int64:
		return d, true
	case u64Value:
		return int64(d), true
	default:
		return 0, false
	}
}

// intRepr tags which of the engine's integer ValueRepr variants a bigIntValue
// stands for. A bigIntValue is always outside int64; inside int64 the repr is
// the payload type itself, int64 or u64Value.
type intRepr uint8

const (
	// reprU64 is ValueRepr::U64 — an integer literal in (i64::MAX, u64::MAX].
	reprU64 intRepr = iota
	// reprI128 is ValueRepr::I128 — the result of checked i128 arithmetic that
	// did not fit i64.
	reprI128
	// reprU128 is ValueRepr::U128 — an integer literal past u64::MAX. Only
	// this variant can hold a magnitude past i128::MAX.
	reprU128
)

var (
	bigZero    = big.NewInt(0)
	bigOne     = big.NewInt(1)
	bigNegOne  = big.NewInt(-1)
	bigU64Max  = new(big.Int).SetUint64(math.MaxUint64)
	bigI128Min = new(big.Int).Neg(new(big.Int).Lsh(bigOne, 127)) // -2^127
	bigI128Max = new(big.Int).Sub(new(big.Int).Lsh(bigOne, 127), bigOne)
	bigU128Max = new(big.Int).Sub(new(big.Int).Lsh(bigOne, 128), bigOne)
	big2Pow128 = new(big.Int).Lsh(bigOne, 128)

	// minI128AsPosU128 is ops.rs:4 MIN_I128_AS_POS_U128 — the one u128 `neg`
	// answers with itself rather than an overflow.
	minI128AsPosU128 = new(big.Int).Lsh(bigOne, 127)
)

// reprForLiteral picks the ValueRepr an integer literal of this magnitude is
// lexed into: `u64` when it fits, `u128` otherwise (compiler/lexer.rs:481-491).
//
// A magnitude the engine cannot hold at all — negative past i128::MIN, or past
// u128::MAX — has no counterpart variant. It is tagged reprU128 so that every
// conversion refuses it, which is the closest the fork can come to a number
// the engine could never have constructed.
func reprForLiteral(x *big.Int) intRepr {
	if x.Sign() >= 0 && x.Cmp(bigU64Max) <= 0 {
		return reprU64
	}
	return reprU128
}

// intBounds returns the saturation bounds of a repr's own Rust type, which is
// what `as_f64`'s lossless round-trip check casts back to.
func intBounds(r intRepr) (lo, hi *big.Int) {
	switch r {
	case reprU64:
		return bigZero, bigU64Max
	case reprU128:
		return bigZero, bigU128Max
	default:
		return bigI128Min, bigI128Max
	}
}

// ---------------------------------------------------------------------------
// Deterministic float-to-integer conversion
// ---------------------------------------------------------------------------

// satCastI64 is Rust's `f64 as i64` cast: NaN becomes 0, the value is
// truncated toward zero, and the result saturates at the i64 bounds.
//
// Go's `int64(f)` is implementation-defined when f is out of range (it
// saturates on arm64 and wraps on amd64), which is the whole reason this
// function exists: every float-to-int conversion in the engine must produce
// the same answer on every architecture.
func satCastI64(d float64) int64 {
	switch {
	case math.IsNaN(d):
		return 0
	// 2^63 is the first f64 at or past i64::MAX; i64::MIN is exactly an f64.
	case d >= 9223372036854775808.0:
		return math.MaxInt64
	case d <= -9223372036854775808.0:
		return math.MinInt64
	default:
		return int64(math.Trunc(d))
	}
}

// satCastU64 is Rust's `f64 as u64` cast: NaN and every negative value become
// 0, and the result saturates at u64::MAX.
//
// This is the cast that makes a U64 distinguishable from an I64: the f64 image
// of i64::MAX is 2^63, which saturates back to i64::MAX through i64 but casts
// back to exactly 2^63 through u64.
func satCastU64(d float64) uint64 {
	switch {
	case math.IsNaN(d), d <= 0:
		return 0
	// 2^64 is the first f64 at or past u64::MAX.
	case d >= 18446744073709551616.0:
		return math.MaxUint64
	default:
		return uint64(math.Trunc(d))
	}
}

// satCastBig is Rust's `f64 as <integer type>` cast for a type whose bounds
// are [lo, hi]: NaN is 0, the value truncates toward zero and saturates.
func satCastBig(d float64, lo, hi *big.Int) *big.Int {
	switch {
	case math.IsNaN(d):
		return new(big.Int)
	case math.IsInf(d, 1):
		return new(big.Int).Set(hi)
	case math.IsInf(d, -1):
		return new(big.Int).Set(lo)
	}
	i, _ := new(big.Float).SetFloat64(d).Int(nil) // truncates toward zero
	if i.Cmp(lo) < 0 {
		return new(big.Int).Set(lo)
	}
	if i.Cmp(hi) > 0 {
		return new(big.Int).Set(hi)
	}
	return i
}

// f64ToI64 is the float arm of `impl TryFrom<Value> for i64`
// (argtypes.rs:410-422):
//
//	ValueRepr::F64(val) if (val as i64 as f64 == val) => val as i64
//
// The guard is a round-trip through the SATURATING cast, not a range test, so
// exactly 2^63 converts (to i64::MAX) while 2^64 does not, and every
// non-integral, NaN or infinite value is refused.
func f64ToI64(d float64) (int64, bool) {
	i := satCastI64(d)
	if float64(i) == d {
		return i, true
	}
	return 0, false
}

// bigToF64 is Rust's `<integer> as f64` cast: round to nearest, ties to even.
func bigToF64(x *big.Int) float64 {
	f, _ := new(big.Float).SetInt(x).Float64()
	return f
}

// ---------------------------------------------------------------------------
// The engine's value conversions
// ---------------------------------------------------------------------------

// AsBigInt is `impl TryFrom<Value> for i128` (argtypes.rs:410-433), the
// conversion every integer-consuming builtin in the engine goes through.
//
// It differs from [Value.AsInt] in exactly one way — its range is i128 rather
// than i64 — which matters for a value past int64: `9223372036854775808 is
// even` is decided here and refused by AsInt.
//
// A bool converts (false is 0, true is 1). A float converts only when it
// round-trips through the saturating i64 cast, which is the engine's own rule
// and not a widening of it: the engine's i128 conversion also goes through the
// i64 arm for floats. A u128 past i128::MAX does not convert at all.
//
// The returned *big.Int is not aliased with the value's payload and is safe to
// mutate.
func (v Value) AsBigInt() (*big.Int, bool) {
	switch d := v.data.(type) {
	case bool:
		if d {
			return big.NewInt(1), true
		}
		return new(big.Int), true
	case int64:
		return big.NewInt(d), true
	case u64Value:
		return big.NewInt(int64(d)), true
	case float64:
		i, ok := f64ToI64(d)
		if !ok {
			return nil, false
		}
		return big.NewInt(i), true
	case bigIntValue:
		if d.Int.Cmp(bigI128Max) > 0 || d.Int.Cmp(bigI128Min) < 0 {
			return nil, false
		}
		return new(big.Int).Set(d.Int), true
	default:
		return nil, false
	}
}

// AsBigUint is `impl TryFrom<Value> for u128` (value/argtypes.rs:410-428): the
// same arms as [Value.AsBigInt], bounded by [0, u128::MAX] instead of by the
// i128 range.
//
// It exists because the engine's integer formatter tries BOTH: `i128` first,
// then `u128` (format_utils.rs:172-184). Without the second try, a `u128` above
// `i128::MAX` is not an integer to the formatter at all and falls through to
// floating point, which is a different number.
//
// The returned *big.Int is not aliased with the value's payload and is safe to
// mutate.
func (v Value) AsBigUint() (*big.Int, bool) {
	switch d := v.data.(type) {
	case bool:
		if d {
			return big.NewInt(1), true
		}
		return new(big.Int), true
	case int64:
		if d < 0 {
			return nil, false
		}
		return big.NewInt(d), true
	case u64Value:
		return big.NewInt(int64(d)), true
	case float64:
		i, ok := f64ToI64(d)
		if !ok || i < 0 {
			return nil, false
		}
		return big.NewInt(i), true
	case bigIntValue:
		if d.Int.Sign() < 0 || d.Int.Cmp(bigU128Max) > 0 {
			return nil, false
		}
		return new(big.Int).Set(d.Int), true
	default:
		return nil, false
	}
}

// AsUsize is `impl TryFrom<Value> for usize` on a 64-bit target, where `usize`
// is a `u64` and its range is [0, u64::MAX].
//
// The engine builds every integer ArgType from one macro, and the target type
// is not decoration: each ValueRepr arm produces a value of that repr's own
// Rust type, and `TryFrom` for the DECLARED type is what bounds it
// (value/argtypes.rs:409-435). So `usize` and `i64` differ over the whole upper
// half of `u64`: `[]|batch(9223372036854775808)` converts, where the same
// literal is refused by an `i64` or `isize` parameter.
//
// The arms are the engine's own. A bool is 0 or 1. An i64 converts when it is
// non-negative. A float converts only when it round-trips through the
// saturating i64 cast — which is why `9223372036854775808.0` converts to
// i64::MAX rather than being refused, exactly as `val as i64 as f64 == val`
// accepts it. A u64 always converts. An i128/u128 converts when it lands inside
// [0, u64::MAX].
//
// The 64-bit assumption is the differential's: both recorded architectures are
// 64-bit, and a 32-bit target would need its own measurement rather than a
// guess. See PATCHES.md #82.
func (v Value) AsUsize() (uint64, bool) {
	switch d := v.data.(type) {
	case bool:
		if d {
			return 1, true
		}
		return 0, true
	case int64:
		if d < 0 {
			return 0, false
		}
		return uint64(d), true
	case u64Value:
		// The payload is a u64 in [0, i64::MAX]; a larger one is bigIntValue.
		return uint64(d), true
	case float64:
		i, ok := f64ToI64(d)
		if !ok || i < 0 {
			return 0, false
		}
		return uint64(i), true
	case bigIntValue:
		if d.Int.Sign() < 0 || d.Int.Cmp(bigU64Max) > 0 {
			return 0, false
		}
		return d.Int.Uint64(), true
	default:
		return 0, false
	}
}

// FromI128 builds a Value from an exact integer, reporting whether it is
// inside the range the engine can hold.
//
// It is `int_as_value` (ops.rs:232-238) preceded by the range check that every
// `checked_*` method in the engine performs: a result outside i128 has no
// representation, and the operation that produced it is an error rather than a
// wider or wrapped answer. Builtins that do exact integer arithmetic of their
// own — `abs` is the one in the default set — go through this so they cannot
// invent a value the operators could not.
//
// The argument is not retained.
func FromI128(x *big.Int) (Value, bool) {
	if !inI128(x) {
		return Undefined(), false
	}
	return fromExactInt(new(big.Int).Set(x)), true
}

// F64ToI128 is Rust's `f64 as i128` cast, which is how the `int` filter
// converts a float (filters.rs:570). NaN becomes 0, the value truncates toward
// zero, and the result saturates at the i128 bounds — so `1e30|int` is exact
// and `1e40|int` is i128::MAX, on every architecture.
func F64ToI128(d float64) *big.Int {
	return satCastBig(d, bigI128Min, bigI128Max)
}

// asF64 is `ops::as_f64` (ops.rs:29-50).
//
// `lossy` says whether a conversion that loses precision is acceptable.
// Arithmetic coerces lossily (`coerce(lhs, rhs, true)`); equality and ordering
// do not (`coerce(self, other, false)`), which is why
// `9007199254740993 == 9007199254740992.0` is false rather than true: the
// integer does not survive the round trip, so the two never reach a comparison
// at all.
//
// The round-trip check casts back to the operand's OWN Rust type with Rust's
// saturating cast, so `i64::MAX` converts losslessly to 2^63.
func asF64(v Value, lossy bool) (float64, bool) {
	switch d := v.data.(type) {
	case bool:
		// ValueRepr::Bool(x) => x as i64 as f64 — never checked.
		if d {
			return 1, true
		}
		return 0, true
	case int64:
		f := float64(d)
		if lossy || satCastI64(f) == d {
			return f, true
		}
		return 0, false
	case u64Value:
		// The one place a U64 is not an I64. `checked!(x, u64)` casts back
		// through u64, so i64::MAX does NOT survive the round trip here even
		// though it does as an I64.
		f := float64(d)
		if lossy || satCastU64(f) == uint64(d) {
			return f, true
		}
		return 0, false
	case float64:
		return d, true
	case bigIntValue:
		f := bigToF64(d.Int)
		if lossy {
			return f, true
		}
		lo, hi := intBounds(d.repr)
		if satCastBig(f, lo, hi).Cmp(d.Int) == 0 {
			return f, true
		}
		return 0, false
	default:
		return 0, false
	}
}

// ---------------------------------------------------------------------------
// coerce
// ---------------------------------------------------------------------------

type coerceKind uint8

const (
	coerceNone coerceKind = iota
	coerceI128
	coerceF64
	coerceStr
)

// coerced is one `ops::CoerceResult`.
//
// The I128 arm keeps a fast path: when both operands already fit int64 the
// pair is carried as int64 and no big.Int is allocated, which is the
// overwhelmingly common case. `bigs` materializes them only when an operator
// actually needs the wide form.
type coerced struct {
	kind coerceKind

	small  bool
	ai, bi int64
	aBig   *big.Int
	bBig   *big.Int

	af, bf float64
	as, bs string
}

func (c coerced) bigs() (*big.Int, *big.Int) {
	if c.small {
		return big.NewInt(c.ai), big.NewInt(c.bi)
	}
	return c.aBig, c.bBig
}

// wrapU128ToI128 is Rust's `u128 as i128`, a wrapping reinterpretation of the
// same 128 bits. It is reachable only through coerce's (U128, U128) arm.
func wrapU128ToI128(x *big.Int) *big.Int {
	if x.Cmp(bigI128Max) <= 0 {
		return new(big.Int).Set(x)
	}
	return new(big.Int).Sub(x, big2Pow128)
}

// coerceValues is `ops::coerce` (ops.rs:52-79), arm for arm.
//
// The engine's first five arms are same-repr pairs. Four of them — (U64,U64),
// (I64,I64), (I128,I128) and the string pairs — produce exactly what the
// general `i128::try_from` arm at the bottom would produce, so they need no
// separate case here. (U128,U128) is the exception and is matched first: it
// casts both operands to i128 by WRAPPING rather than refusing them, so a pair
// of literals past i128::MAX becomes a pair of negative i128 values.
func coerceValues(a, b Value, lossy bool) coerced {
	if ab, ok := a.data.(bigIntValue); ok && ab.repr == reprU128 {
		if bb, ok := b.data.(bigIntValue); ok && bb.repr == reprU128 {
			return coerced{kind: coerceI128, aBig: wrapU128ToI128(ab.Int), bBig: wrapU128ToI128(bb.Int)}
		}
	}

	// String pairs. The engine matches these ahead of the float arms, so a
	// (string, float) pair falls through to the float arm and refuses there.
	if as, ok := a.AsString(); ok {
		if bs, ok := b.AsString(); ok {
			return coerced{kind: coerceStr, as: as, bs: bs}
		}
	}

	// Are floats involved? One float operand puts the whole operation in f64.
	if af, ok := a.data.(float64); ok {
		bf, ok := asF64(b, lossy)
		if !ok {
			return coerced{}
		}
		return coerced{kind: coerceF64, af: af, bf: bf}
	}
	if bf, ok := b.data.(float64); ok {
		af, ok := asF64(a, lossy)
		if !ok {
			return coerced{}
		}
		return coerced{kind: coerceF64, af: af, bf: bf}
	}

	// Everything else goes up to i128. The engine reaches the same i128 pair
	// for an (I64, I64), a (U64, U64) and a mixed pair — the first two through
	// their own coerce arms, the mixed one through `i128::try_from` — so the
	// fast path asks only whether both operands fit int64.
	if ai, ok := smallInt(a); ok {
		if bi, ok := smallInt(b); ok {
			return coerced{kind: coerceI128, small: true, ai: ai, bi: bi}
		}
	}
	aBig, ok := a.AsBigInt()
	if !ok {
		return coerced{}
	}
	bBig, ok := b.AsBigInt()
	if !ok {
		return coerced{}
	}
	return coerced{kind: coerceI128, aBig: aBig, bBig: bBig}
}

// ---------------------------------------------------------------------------
// Checked i128 arithmetic
// ---------------------------------------------------------------------------

// inI128 reports whether an exact result is inside the engine's i128 range.
// Every `checked_*` method the engine uses returns None outside it.
func inI128(x *big.Int) bool {
	return x.Cmp(bigI128Min) >= 0 && x.Cmp(bigI128Max) <= 0
}

// fromExactInt is `int_as_value` (ops.rs:232-238): narrow to i64 when the
// value fits, otherwise keep it as ValueRepr::I128.
//
// Arithmetic never produces a U64 or U128; only a literal does.
func fromExactInt(x *big.Int) Value {
	if x.IsInt64() {
		return FromInt(x.Int64())
	}
	return Value{data: bigIntValue{Int: x, repr: reprI128}}
}

// failedOp is `ops::failed_op` (ops.rs:252-257): an operation whose operands
// were coerced but whose checked arithmetic overflowed or divided by zero.
func failedOp(op string, lhs, rhs Value) error {
	return fmt.Errorf("unable to calculate %s %s %s", lhs.String(), op, rhs.String())
}

// impossibleOp is `ops::impossible_op` (ops.rs:240-250): an operation whose
// operands do not coerce to a common type at all.
//
// The wording is the engine's rather than the port's, because BAML surfaces the
// engine's message to a caller when a prompt fails to render, and
// oracle/messages_test.go compares the two.
func impossibleOp(op string, lhs, rhs Value) error {
	return fmt.Errorf("tried to use %s operator on unsupported types %s and %s",
		op, lhs.Kind(), rhs.Kind())
}

// ErrInvalidOperationNoDetail is the engine's
// `Error::from(ErrorKind::InvalidOperation)`: an invalid operation carrying NO
// detail, which renders as just "invalid operation" rather than
// "invalid operation: something".
//
// The VM turns a plain error from this package into an invalid-operation error
// with the error's text as the detail. This sentinel is how it is told that
// there is no detail — explicitly, rather than by inferring it from an empty
// message, because an empty detail is still a detail and other errors can
// legitimately have one. Match it with errors.Is.
//
// [Value.Neg] is the only operation that raises it, because `ops::neg` is the
// only numeric operation the engine reports without a detail.
var ErrInvalidOperationNoDetail = errors.New("invalid operation")

// checkedPowI128 is `i128::checked_pow`, transcribed so that an exponent that
// would overflow is rejected after a bounded number of squarings rather than
// by materializing an astronomically large big.Int.
func checkedPowI128(base *big.Int, exp uint32) (*big.Int, bool) {
	if exp == 0 {
		return big.NewInt(1), true
	}
	acc := big.NewInt(1)
	b := new(big.Int).Set(base)
	mul := func(x, y *big.Int) (*big.Int, bool) {
		r := new(big.Int).Mul(x, y)
		if !inI128(r) {
			return nil, false
		}
		return r, true
	}
	for {
		if exp&1 == 1 {
			var ok bool
			if acc, ok = mul(acc, b); !ok {
				return nil, false
			}
			if exp == 1 {
				return acc, true
			}
		}
		exp /= 2
		var ok bool
		if b, ok = mul(b, b); !ok {
			return nil, false
		}
	}
}

// divEuclidF64 is Rust's `f64::div_euclid`, which the engine uses for `//` on
// floats. It is NOT `math.Floor(a / b)`: the two disagree whenever the
// remainder is zero and the signs differ, so `7.0 // -2.0` is -3 here and -4
// under floor division.
func divEuclidF64(a, b float64) float64 {
	q := math.Trunc(a / b)
	if math.Mod(a, b) < 0 {
		if b > 0 {
			return q - 1
		}
		return q + 1
	}
	return q
}

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

// formatFloat is the engine's `Display for Value` float arm (value/mod.rs:697-709):
// Rust's shortest round-tripping decimal, never in exponent form, with ".0"
// appended when the result has no fractional part.
//
// Go's `%g` produced six significant digits, so `1 / 3` rendered "0.333333"
// against the engine's "0.3333333333333333"; `'f'` with precision -1 is Go's
// shortest round-tripping positional form, which is the same string Rust's
// `f64::to_string` produces. The fork's own lexer already normalizes float
// literals this way (internal/lexer/lexer.go:1178-1182).
func formatFloat(d float64) string {
	switch {
	case math.IsNaN(d):
		return "NaN"
	case math.IsInf(d, 1):
		return "inf"
	case math.IsInf(d, -1):
		return "-inf"
	}
	s := strconv.FormatFloat(d, 'f', -1, 64)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}

// f64TotalCmp is `value::f64_total_cmp` (value/mod.rs:597-604), the engine's
// ordering for two floats. It is a TOTAL order: -0.0 sorts below 0.0 and NaN
// sorts above every number, where Go's `<`/`>` report neither.
func f64TotalCmp(left, right float64) int {
	l := int64(math.Float64bits(left))
	r := int64(math.Float64bits(right))
	l ^= int64(uint64(l>>63) >> 1)
	r ^= int64(uint64(r>>63) >> 1)
	switch {
	case l < r:
		return -1
	case l > r:
		return 1
	default:
		return 0
	}
}
