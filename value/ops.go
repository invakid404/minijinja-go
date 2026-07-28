package value

import (
	"bytes"
	"fmt"
	"math"
	"math/big"
	"strings"
)

// Neg performs unary negation.
//
// This is `ops::neg` (ops.rs:412-435). Negation is checked i128, so negating
// i128::MIN overflows rather than wrapping, and the one u128 that equals
// -i128::MIN answers with itself. A bool is not a Number and cannot be
// negated, matching the engine's `val.kind() == ValueKind::Number` guard.
func (v Value) Neg() (Value, error) {
	if v.Kind() != KindNumber {
		return Undefined(), ErrInvalidOperationNoDetail
	}
	if d, ok := v.data.(float64); ok {
		return FromFloat(-d), nil
	}
	// The special case for the largest i128 that can still be represented.
	if d, ok := v.data.(bigIntValue); ok &&
		d.repr == reprU128 && d.Int.Cmp(minI128AsPosU128) == 0 {
		return Value{data: bigIntValue{Int: new(big.Int).Set(d.Int), repr: reprU128}}, nil
	}
	x, ok := v.AsBigInt()
	if !ok {
		return Undefined(), ErrInvalidOperationNoDetail
	}
	// `x.checked_mul(-1)`.
	x.Neg(x)
	if !inI128(x) {
		return Undefined(), fmt.Errorf("overflow")
	}
	return fromExactInt(x), nil
}

// isActualInt is gone: the "are both operands actual ints?" gate it existed for
// was the port's way of deciding whether an operation returns an integer, and
// the engine decides that in `ops::coerce` on the payload type instead. The
// exported [Value.IsActualInt] remains, because the engine has its own question
// of that shape (`Value::is_integer`) and builtins ask it.

// Add performs addition or string concatenation.
func (v Value) Add(other Value) (Value, error) {
	// String concatenation
	if s1, ok := v.AsString(); ok {
		if s2, ok := other.AsString(); ok {
			if v.IsSafe() || other.IsSafe() {
				return FromSafeString(s1 + s2), nil
			}
			return FromString(s1 + s2), nil
		}
	}

	// Numeric addition: `ops::add` (ops.rs:274-299). Integers are added in
	// checked i128 and overflow is an error, never a wrap.
	switch c := coerceValues(v, other, true); c.kind {
	case coerceI128:
		if c.small {
			if sum := c.ai + c.bi; (sum > c.ai) == (c.bi > 0) {
				return FromInt(sum), nil
			}
		}
		a, b := c.bigs()
		if a.Add(a, b); !inI128(a) {
			return Undefined(), failedOp("+", v, other)
		}
		return fromExactInt(a), nil
	case coerceF64:
		return FromFloat(c.af + c.bf), nil
	}

	// Sequence concatenation. An iterable counts as a sequence here — the Rust
	// engine chains the two iterators and yields a lazy iterable — so adding a
	// slice to a list works.
	if isSeqLike(v.Kind()) && isSeqLike(other.Kind()) {
		s1, s2 := v.Iter(), other.Iter()
		if s1 != nil && s2 != nil {
			result := make([]Value, 0, len(s1)+len(s2))
			result = append(result, s1...)
			result = append(result, s2...)
			return FromIterator(NewIterator("chain", result)), nil
		}
	}

	return Undefined(), impossibleOp("+", v, other)
}

// Sub performs subtraction.
//
// `math_binop!(sub, checked_sub, -)` (ops.rs:301).
func (v Value) Sub(other Value) (Value, error) {
	switch c := coerceValues(v, other, true); c.kind {
	case coerceI128:
		if c.small {
			if diff := c.ai - c.bi; (diff < c.ai) == (c.bi > 0) {
				return FromInt(diff), nil
			}
		}
		a, b := c.bigs()
		if a.Sub(a, b); !inI128(a) {
			return Undefined(), failedOp("-", v, other)
		}
		return fromExactInt(a), nil
	case coerceF64:
		return FromFloat(c.af - c.bf), nil
	}
	return Undefined(), impossibleOp("-", v, other)
}

// Mul performs multiplication.
//
// The two repetition branches are container operations rather than arithmetic —
// the Rust engine handles them before any numeric coercion — so their count is
// converted with the engine's primitive integer conversion ([Value.AsInt]),
// which accepts a bool as 0/1 and an integral float. A negative count does not
// convert there (it is a usize), which the `n >= 0` guards mirror.
func (v Value) Mul(other Value) (Value, error) {
	// String repetition. `ops::mul` (ops.rs:304-315) commits to this arm as soon
	// as EITHER operand is a string, and reports a count it cannot read as a
	// usize right there. Falling through to the numeric arm instead — which is
	// what the port did — reported the operand KINDS as unsupported, when the
	// kinds are fine and it is the count that is not.
	if s, str, ok := stringOperand(v, other); ok {
		n, ok := s.AsInt()
		if !ok || n < 0 {
			return Undefined(), fmt.Errorf("strings can only be multiplied with integers")
		}
		if str.IsSafe() {
			return FromSafeString(strings.Repeat(str.mustString(), int(n))), nil
		}
		return FromString(strings.Repeat(str.mustString(), int(n))), nil
	}

	// Sequence/Iterator repetition, the same way round: the engine commits to
	// this arm on the operand KIND, so a count it cannot read is reported as a
	// bad count rather than falling through to numeric multiplication and
	// reporting the operand kinds as unsupported.
	if v.Kind() == KindSeq || v.Kind() == KindIterable {
		return repeatIterable(v, other)
	}
	if other.Kind() == KindSeq || other.Kind() == KindIterable {
		return repeatIterable(other, v)
	}

	// Numeric multiplication: `ops::mul` (ops.rs:304-333).
	switch c := coerceValues(v, other, true); c.kind {
	case coerceI128:
		if c.small {
			// The int64 fast path is only taken when the product is provably
			// exact; otherwise the wide form decides.
			if prod := c.ai * c.bi; c.ai == 0 || (prod/c.ai == c.bi && !(c.ai == -1 && c.bi == math.MinInt64)) {
				return FromInt(prod), nil
			}
		}
		a, b := c.bigs()
		if a.Mul(a, b); !inI128(a) {
			return Undefined(), failedOp("*", v, other)
		}
		return fromExactInt(a), nil
	case coerceF64:
		return FromFloat(c.af * c.bf), nil
	}

	return Undefined(), impossibleOp("*", v, other)
}

// stringOperand implements the engine's
// `lhs.as_str().map(|s| (s, rhs)).or_else(|| rhs.as_str().map(|s| (s, lhs)))`:
// it picks the string operand — left first — and returns the OTHER one as the
// repetition count.
func stringOperand(lhs, rhs Value) (count, str Value, ok bool) {
	if _, isStr := lhs.AsString(); isStr {
		return rhs, lhs, true
	}
	if _, isStr := rhs.AsString(); isStr {
		return lhs, rhs, true
	}
	return Undefined(), Undefined(), false
}

// mustString is AsString for a value already known to be a string.
func (v Value) mustString() string {
	s, _ := v.AsString()
	return s
}

func repeatIterable(seq Value, count Value) (Value, error) {
	n, ok := count.AsInt()
	if !ok || n < 0 {
		return Undefined(), fmt.Errorf("sequences and iterables can only be multiplied with integers")
	}
	if _, ok := seq.Len(); !ok {
		return Undefined(), fmt.Errorf("cannot repeat unsized iterables")
	}

	items := seq.Iter()
	if items == nil {
		return Undefined(), fmt.Errorf("cannot repeat unsized iterables")
	}

	result := make([]Value, 0, len(items)*int(n))
	for i := int64(0); i < n; i++ {
		result = append(result, items...)
	}
	// A repeated sequence is a lazy iterable, like a slice and like sequence
	// concatenation. It renders as a list but is not a sequence, which
	// `is sequence` observes.
	return FromIterator(NewIterator("repeat", result)), nil
}

// Div performs division.
//
// `ops::div` (ops.rs:373-380) is unconditionally f64: two actual integers still
// produce a float, and division by zero yields ±inf or NaN rather than an
// error. It does not go through `coerce` at all — it reads both operands with
// `as_f64(_, true)` directly — so a bool operand divides as 0 or 1.
func (v Value) Div(other Value) (Value, error) {
	if f1, ok := asF64(v, true); ok {
		if f2, ok := asF64(other, true); ok {
			return FromFloat(f1 / f2), nil
		}
	}
	return Undefined(), impossibleOp("/", v, other)
}

// FloorDiv performs floor division.
//
// `ops::int_div` (ops.rs:382-396). Integers divide with `checked_div_euclid`
// and floats with `f64::div_euclid` — Euclidean, not floored: the two agree on
// `-7 // 2` and disagree on `7 // -2`, which is -3 here and -4 under floor
// division.
func (v Value) FloorDiv(other Value) (Value, error) {
	switch c := coerceValues(v, other, true); c.kind {
	case coerceI128:
		if c.small && c.bi != 0 && !(c.ai == math.MinInt64 && c.bi == -1) {
			// i64::div_euclid, transcribed. It cannot overflow here: the only
			// quotient that would is the case excluded above.
			q := c.ai / c.bi
			if c.ai%c.bi < 0 {
				if c.bi > 0 {
					q--
				} else {
					q++
				}
			}
			return FromInt(q), nil
		}
		a, b := c.bigs()
		if b.Sign() == 0 {
			return Undefined(), failedOp("//", v, other)
		}
		// big.Int.Div is Euclidean, which is exactly checked_div_euclid; the
		// one overflowing case (i128::MIN // -1) is caught by the range check.
		if a.Div(a, b); !inI128(a) {
			return Undefined(), failedOp("//", v, other)
		}
		return fromExactInt(a), nil
	case coerceF64:
		return FromFloat(divEuclidF64(c.af, c.bf)), nil
	}
	return Undefined(), impossibleOp("//", v, other)
}

// Rem performs the modulo operation.
//
// `math_binop!(rem, checked_rem_euclid, %)` (ops.rs:302). The integer arm is
// EUCLIDEAN, so the result is never negative — `-7 % 2` is 1, not Go's -1 —
// while the float arm is the ordinary truncated remainder, which is what
// math.Mod computes.
func (v Value) Rem(other Value) (Value, error) {
	switch c := coerceValues(v, other, true); c.kind {
	case coerceI128:
		if c.small && c.bi != 0 && !(c.ai == math.MinInt64 && c.bi == -1) {
			// i64::rem_euclid, transcribed.
			r := c.ai % c.bi
			if r < 0 {
				if c.bi > 0 {
					r += c.bi
				} else {
					r -= c.bi
				}
			}
			return FromInt(r), nil
		}
		a, b := c.bigs()
		// checked_rem_euclid returns None on a zero divisor and on the one
		// case that overflows, i128::MIN % -1.
		if b.Sign() == 0 || (a.Cmp(bigI128Min) == 0 && b.Cmp(bigNegOne) == 0) {
			return Undefined(), failedOp("%", v, other)
		}
		// big.Int.Mod is Euclidean: the result is always in [0, |b|).
		return fromExactInt(a.Mod(a, b)), nil
	case coerceF64:
		return FromFloat(math.Mod(c.af, c.bf)), nil
	}
	return Undefined(), impossibleOp("%", v, other)
}

// Pow performs exponentiation.
//
// `ops::pow` (ops.rs:398-410). Integer exponentiation is `i128::checked_pow`,
// whose exponent is a `u32`: a negative, fractional or oversized exponent is an
// error rather than a silent fallback to floating point. A float on either side
// puts the whole operation in `f64::powf`, so `2.0 ** 63` is a float here and
// is not converted back to an integer.
func (v Value) Pow(other Value) (Value, error) {
	switch c := coerceValues(v, other, true); c.kind {
	case coerceI128:
		a, b := c.bigs()
		// `TryFrom::<u32>::try_from(b)` — negative and oversized exponents fail.
		if !b.IsUint64() || b.Uint64() > math.MaxUint32 {
			return Undefined(), failedOp("**", v, other)
		}
		r, ok := checkedPowI128(a, uint32(b.Uint64()))
		if !ok {
			return Undefined(), failedOp("**", v, other)
		}
		return fromExactInt(r), nil
	case coerceF64:
		return FromFloat(math.Pow(c.af, c.bf)), nil
	}
	return Undefined(), impossibleOp("**", v, other)
}

// Equal returns true if two values are equal.
//
// This follows the Rust engine's PartialEq for Value exactly
// (boundaryml/minijinja@8cfc770 value/mod.rs): identical-repr fast paths, then
// comparison coercion (which is non-lossy and has no number/string rule), then
// the generic value_cmp hook from either operand, then container and object
// comparison by kind.
func (v Value) Equal(other Value) bool {
	// Fast paths for identical reprs, matching Rust's first match arms. A pair
	// that is NOT both none or both undefined deliberately falls through rather
	// than answering false, so it can still reach the value_cmp hook below.
	// Bytes need their own arm because coercion has no rule for them.
	switch {
	case v.IsNone() || other.IsNone():
		if v.IsNone() && other.IsNone() {
			return true
		}
	case v.IsUndefined() || other.IsUndefined():
		if v.IsUndefined() && other.IsUndefined() {
			return true
		}
	}
	if b1, ok := v.data.([]byte); ok {
		if b2, ok := other.data.([]byte); ok {
			return bytes.Equal(b1, b2)
		}
	}

	// Numbers, bools and strings all reach the engine's PartialEq fallthrough,
	// `ops::coerce(self, other, false)` (value/mod.rs:496-507).
	//
	// The coercion is LOSSLESS here, which is what keeps distinct integers
	// distinct: 9007199254740993 and 9007199254740992 compare in i128 and are
	// unequal, where routing both through float64 collapsed them onto one
	// value. And an integer that cannot survive the round trip to f64 never
	// reaches a comparison with a float at all, so
	// `9007199254740993 == 9007199254740992.0` is false rather than true.
	switch c := coerceValues(v, other, false); c.kind {
	case coerceI128:
		if c.small {
			return c.ai == c.bi
		}
		a, b := c.bigs()
		return a.Cmp(b) == 0
	case coerceF64:
		return c.af == c.bf
	case coerceStr:
		return c.as == c.bs
	}

	// The generic value_cmp hook, tried from the left operand and then from the
	// right for commutativity. This is BoundaryML's engine delta.
	if cmp, ok := valueCmpBothWays(v, other, false); ok {
		return cmp == 0
	}

	return v.containerEqual(other)
}

// containerEqual is the object/container arm of equality: what Rust reaches once
// coercion and value_cmp have both declined.
func (v Value) containerEqual(other Value) bool {
	if obj1, ok := v.AsObject(); ok {
		if obj2, ok := other.AsObject(); ok {
			if obj1 == obj2 {
				return true
			}
			if cmp, ok := CompareObjects(obj1, obj2); ok {
				return cmp == 0
			}
		}
	}

	switch k1, k2 := v.Kind(), other.Kind(); {
	case k1 == KindMap && k2 == KindMap:
		keys1, _ := v.MapKeys()
		keys2, _ := other.MapKeys()
		if len(keys1) != len(keys2) {
			return false
		}
		for _, k := range keys1 {
			val1, ok1 := mapLookup(v, k)
			val2, ok2 := mapLookup(other, k)
			if !ok1 || !ok2 || !val1.Equal(val2) {
				return false
			}
		}
		return true
	case isSeqLike(k1) && isSeqLike(k2):
		// A sequence and an iterable with the same items are equal: Rust
		// compares their iterators, not their reprs.
		items1, items2 := v.Iter(), other.Iter()
		if len(items1) != len(items2) {
			return false
		}
		for i := range items1 {
			if !items1[i].Equal(items2[i]) {
				return false
			}
		}
		return true
	case isPlainLike(k1) && isPlainLike(k2):
		// Rust's own comment calls this a terrible fallback, but it is the
		// behaviour: two plain objects compare by their rendering.
		return v.String() == other.String()
	default:
		return false
	}
}

// UncomparableNumbers is the value [Value.Compare] panics with when two numbers
// cannot be coerced losslessly to a common type — an integer past 2^53 ordered
// against a float, or a u128 past i128::MAX ordered against anything that is not
// also a u128.
//
// It exists because the engine panics there. `Ord::cmp` handles a failed
// coercion by assuming both operands are objects and unwrapping
// `self.as_object()` (value/mod.rs:638-641); for two numbers that unwrap is on
// a None. Returning an error instead would be a different observable outcome
// from the engine's on an input BAML can reach, and this fork's contract is to
// reproduce the engine rather than to improve on it.
//
// It is a distinct type, and an error, so a host that wants to survive the
// input can recover and identify it:
//
//	defer func() {
//	    if r := recover(); r != nil {
//	        if u, ok := r.(value.UncomparableNumbers); ok { ... }
//	    }
//	}()
//
// Only ordering panics. [Value.Equal] reaches the same failed coercion and
// returns false, because the engine's `PartialEq` handles it safely
// (value/mod.rs:504-508) — so `a == b` is always answerable even where
// `a < b` is not.
type UncomparableNumbers struct {
	Left, Right Value
}

func (u UncomparableNumbers) Error() string {
	return fmt.Sprintf("cannot order %s against %s: neither converts to the other without loss",
		u.Left.String(), u.Right.String())
}

// Compare returns -1 if v < other, 0 if equal, 1 if v > other.
//
// This follows the Rust engine's Ord for Value (boundaryml/minijinja@8cfc770
// value/mod.rs). Three consequences are worth naming because they differ from a
// naive implementation:
//
//   - The generic value_cmp hook runs before kind ordering, so an object can
//     order itself against a string or a number.
//   - Floats are ordered with a total order (f64::total_cmp), not with `<`. NaN
//     is therefore ordered rather than unordered, and -0.0 sorts below 0.0 even
//     though they are equal.
//   - It PANICS with [UncomparableNumbers] on the one input the engine panics
//     on; see that type.
//
// ok is false only for a pair the engine has no rule for at all, which is the
// position where Rust's implementation is unreachable.
func (v Value) Compare(other Value) (int, bool) {
	// Before kind ordering, so cross-type object comparison is possible.
	if cmp, ok := valueCmpBothWays(v, other, true); ok {
		return cmp, true
	}

	if k1, k2 := kindOrder(v.Kind()), kindOrder(other.Kind()); k1 != k2 {
		if k1 < k2 {
			return -1, true
		}
		return 1, true
	}

	// Same kind. Kinds with no payload to compare are equal to each other.
	if v.IsNone() && other.IsNone() {
		return 0, true
	}
	if v.IsUndefined() && other.IsUndefined() {
		return 0, true
	}
	if b1, ok := v.data.([]byte); ok {
		if b2, ok := other.data.([]byte); ok {
			return bytes.Compare(b1, b2), true
		}
	}

	// Bools, numbers and strings reach the engine's `Ord` fallthrough,
	// `ops::coerce(self, other, false)` (value/mod.rs:634-637). Integers are
	// ordered in i128 rather than through float64, and floats are ordered by
	// `f64_total_cmp`, a TOTAL order in which -0.0 sorts below 0.0 and NaN
	// sorts above every number.
	c := coerceValues(v, other, false)

	// When that coercion yields nothing, the engine's `Ord` assumes both
	// operands must therefore be objects and calls `self.as_object().unwrap()`
	// (value/mod.rs:638-641). For two NUMBERS the assumption is false and the
	// engine PANICS, which is reproduced here — see [UncomparableNumbers].
	if c.kind == coerceNone && v.Kind() == KindNumber && other.Kind() == KindNumber {
		panic(UncomparableNumbers{Left: v, Right: other})
	}

	switch c.kind {
	case coerceI128:
		if c.small {
			switch {
			case c.ai < c.bi:
				return -1, true
			case c.ai > c.bi:
				return 1, true
			}
			return 0, true
		}
		a, b := c.bigs()
		return a.Cmp(b), true
	case coerceF64:
		return f64TotalCmp(c.af, c.bf), true
	case coerceStr:
		return strings.Compare(c.as, c.bs), true
	}

	return v.containerCompare(other)
}

// containerCompare is the object/container arm of ordering.
func (v Value) containerCompare(other Value) (int, bool) {
	if obj1, ok := v.AsObject(); ok {
		if obj2, ok := other.AsObject(); ok {
			if obj1 == obj2 {
				return 0, true
			}
			if cmp, ok := CompareObjects(obj1, obj2); ok {
				return cmp, true
			}
		}
	}

	switch k1, k2 := v.Kind(), other.Kind(); {
	case k1 == KindMap && k2 == KindMap:
		// Ordering compares key/value pairs in iteration order. Rust documents
		// that this makes the result depend on insertion order and accepts it
		// rather than paying to sort.
		keys1, _ := v.MapKeys()
		keys2, _ := other.MapKeys()
		for i := 0; i < len(keys1) && i < len(keys2); i++ {
			if cmp := strings.Compare(keys1[i], keys2[i]); cmp != 0 {
				return cmp, true
			}
			val1, _ := mapLookup(v, keys1[i])
			val2, _ := mapLookup(other, keys2[i])
			if cmp, ok := val1.Compare(val2); ok && cmp != 0 {
				return cmp, true
			}
		}
		return compareLengths(len(keys1), len(keys2)), true
	case isSeqLike(k1) && isSeqLike(k2):
		items1, items2 := v.Iter(), other.Iter()
		for i := 0; i < len(items1) && i < len(items2); i++ {
			if cmp, ok := items1[i].Compare(items2[i]); ok && cmp != 0 {
				return cmp, true
			}
		}
		return compareLengths(len(items1), len(items2)), true
	case isPlainLike(k1) && isPlainLike(k2):
		return strings.Compare(v.String(), other.String()), true
	default:
		return 0, false
	}
}

func compareLengths(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// isSeqLike reports whether a kind is compared by iterating its items. A
// sequence and an iterable are interchangeable here, which is why range(3)
// equals [0, 1, 2].
func isSeqLike(k ValueKind) bool {
	return k == KindSeq || k == KindIterable
}

// isPlainLike reports whether a kind is compared by its rendering. The Rust
// engine has no callable kind — a function is a plain object — so a callable
// belongs here too.
func isPlainLike(k ValueKind) bool {
	return k == KindPlain || k == KindCallable
}

// mapLookup reads one entry of a mapping, whatever backs it.
func mapLookup(v Value, key string) (Value, bool) {
	switch d := v.data.(type) {
	case *OrderedMap:
		return d.Get(key)
	case map[string]Value:
		val, ok := d[key]
		return val, ok
	}
	got := v.GetItem(FromString(key))
	if got.IsUndefined() {
		return got, false
	}
	return got, true
}

// Contains reports whether v contains other.
//
// It answers false rather than reporting an error when v cannot hold values at
// all. Use [Value.TryContains] to get the engine's behaviour, which is to raise:
// the `in` operator must not silently answer false for `1 in 5`.
func (v Value) Contains(other Value) bool {
	got, err := v.TryContains(other)
	if err != nil {
		return false
	}
	return got
}

// TryContains reports whether v contains other, following the Rust engine's
// `contains` (boundaryml/minijinja@8cfc770 value/ops.rs).
//
// The rules are asymmetric on purpose:
//
//   - An undefined container holds nothing: false, not an error.
//   - A string container stringifies a non-string needle, so `1 in '1'` is true.
//   - A mapping tests its keys; a sequence or iterable tests its items by
//     equality (and therefore reaches the value_cmp hook).
//   - Anything else is an error, because it is not a container.
func (v Value) TryContains(other Value) (bool, error) {
	if v.IsUndefined() {
		return false, nil
	}
	if s, ok := v.AsString(); ok {
		if needle, ok := other.AsString(); ok {
			return strings.Contains(s, needle), nil
		}
		return strings.Contains(s, other.String()), nil
	}
	switch v.Kind() {
	case KindMap:
		if keys, ok := v.MapKeys(); ok {
			needle, isStr := other.AsString()
			if !isStr {
				// A mapping is keyed by string here, so a non-string key can
				// never be present. The Rust engine looks the key up as a
				// Value, which for a string-keyed map has the same answer.
				return false, nil
			}
			for _, k := range keys {
				if k == needle {
					return true, nil
				}
			}
		}
		return false, nil
	case KindSeq, KindIterable:
		for _, item := range v.Iter() {
			if item.Equal(other) {
				return true, nil
			}
		}
		return false, nil
	case KindPlain, KindCallable:
		// A plain object holds nothing, but it is still a container as far as
		// the operator is concerned: Rust answers false rather than raising.
		if _, ok := v.AsObject(); ok {
			return false, nil
		}
	}
	// Everything else, bytes included, is not a container: the Rust engine's
	// containment check only knows strings and objects.
	return false, fmt.Errorf("cannot perform a containment check on this value")
}

// Concat performs the tilde (~) string concatenation.
func (v Value) Concat(other Value) Value {
	s1 := v.String()
	s2 := other.String()
	if v.IsSafe() && other.IsSafe() {
		return FromSafeString(s1 + s2)
	}
	return FromString(s1 + s2)
}
