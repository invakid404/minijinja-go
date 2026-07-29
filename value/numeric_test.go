package value

import (
	"math"
	"math/big"
	"math/rand"
	"testing"
)

// The differential oracle is the authority on parity with BAML's engine, but it
// needs a Rust toolchain. These tests pin the same numeric model inside the
// engine's own CGO-free `go test ./...` lane, so a regression is caught by the
// ordinary test run and not only by the oracle job.

// TestSatCastI64IsArchitectureIndependent pins the conversion that made the
// port answer differently on arm64 and amd64.
//
// The property that matters is not just the table below: it is that no
// out-of-range `int64(float64)` conversion is performed at all. Go leaves that
// conversion implementation-defined, so a table alone would pass on one
// architecture and fail on the other. Every case here is computed by explicit
// comparison and truncation instead.
func TestSatCastI64IsArchitectureIndependent(t *testing.T) {
	const twoPow63 = 9223372036854775808.0
	cases := []struct {
		in   float64
		want int64
	}{
		{math.NaN(), 0},
		{math.Inf(1), math.MaxInt64},
		{math.Inf(-1), math.MinInt64},
		{twoPow63, math.MaxInt64},  // i64::MAX rounds UP to 2^63 as an f64
		{-twoPow63, math.MinInt64}, // i64::MIN is exactly an f64
		{twoPow63 * 2, math.MaxInt64},
		{-twoPow63 * 2, math.MinInt64},
		{1e30, math.MaxInt64},
		{-1e30, math.MinInt64},
		{0, 0},
		{-0.0, 0},
		{1.9, 1},
		{-1.9, -1},
		{9007199254740993.0, 9007199254740992}, // the literal's f64 image
	}
	for _, c := range cases {
		if got := satCastI64(c.in); got != c.want {
			t.Errorf("satCastI64(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}

// TestF64ToI64RoundTripGuard pins `TryFrom<Value> for i64`'s float arm: the
// guard is a round trip through the saturating cast, not a range test, which is
// why exactly 2^63 converts and 2^64 does not.
func TestF64ToI64RoundTripGuard(t *testing.T) {
	cases := []struct {
		in   float64
		want int64
		ok   bool
	}{
		{2.0, 2, true},
		{-2.0, -2, true},
		{1.5, 0, false},
		{9223372036854775808.0, math.MaxInt64, true},  // 2^63
		{-9223372036854775808.0, math.MinInt64, true}, // -2^63
		{18446744073709551616.0, 0, false},            // 2^64
		{math.NaN(), 0, false},
		{math.Inf(1), 0, false},
		{math.Inf(-1), 0, false},
	}
	for _, c := range cases {
		got, ok := f64ToI64(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("f64ToI64(%v) = (%d, %v), want (%d, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

// TestIntegerArithmeticIsExact walks the operators over the boundaries the
// float64 path could not represent.
func TestIntegerArithmeticIsExact(t *testing.T) {
	i := func(s string) Value {
		x, ok := new(big.Int).SetString(s, 10)
		if !ok {
			t.Fatalf("bad literal %q", s)
		}
		return FromBigInt(x)
	}
	cases := []struct {
		name string
		got  func() (Value, error)
		want string
	}{
		{"2^53+1 + 1", func() (Value, error) { return i("9007199254740993").Add(FromInt(1)) }, "9007199254740994"},
		{"2^53+1 - 2^53", func() (Value, error) { return i("9007199254740993").Sub(i("9007199254740992")) }, "1"},
		{"i64::MAX - 1", func() (Value, error) { return i("9223372036854775807").Sub(FromInt(1)) }, "9223372036854775806"},
		{"i64::MAX + 1", func() (Value, error) { return i("9223372036854775807").Add(FromInt(1)) }, "9223372036854775808"},
		{"2^62 * 2", func() (Value, error) { return i("4611686018427387904").Mul(FromInt(2)) }, "9223372036854775808"},
		{"i64::MAX squared", func() (Value, error) { return i("9223372036854775807").Mul(i("9223372036854775807")) }, "85070591730234615847396907784232501249"},
		{"7 // -2 is Euclidean", func() (Value, error) { return FromInt(7).FloorDiv(FromInt(-2)) }, "-3"},
		{"-7 // 2", func() (Value, error) { return FromInt(-7).FloorDiv(FromInt(2)) }, "-4"},
		{"-7 % 2 is Euclidean", func() (Value, error) { return FromInt(-7).Rem(FromInt(2)) }, "1"},
		{"7 % -2 is Euclidean", func() (Value, error) { return FromInt(7).Rem(FromInt(-2)) }, "1"},
		{"7 / 2 is a float", func() (Value, error) { return FromInt(7).Div(FromInt(2)) }, "3.5"},
		{"4 / 2 is a float", func() (Value, error) { return FromInt(4).Div(FromInt(2)) }, "2.0"},
		{"1 / 0 is inf", func() (Value, error) { return FromInt(1).Div(FromInt(0)) }, "inf"},
		{"2 ** 126", func() (Value, error) { return FromInt(2).Pow(FromInt(126)) }, "85070591730234615865843651857942052864"},
		{"2.0 ** 63 stays float", func() (Value, error) { return FromFloat(2).Pow(FromInt(63)) }, "9223372036854776000.0"},
		{"4.0 %% 3 stays float", func() (Value, error) { return FromFloat(4).Rem(FromInt(3)) }, "1.0"},
		{"true + 1", func() (Value, error) { return FromBool(true).Add(FromInt(1)) }, "2"},
		{"-i64::MIN widens", func() (Value, error) { return FromInt(math.MinInt64).Neg() }, "9223372036854775808"},
	}
	for _, c := range cases {
		got, err := c.got()
		if err != nil {
			t.Errorf("%s: unexpected error %v", c.name, err)
			continue
		}
		if s := got.String(); s != c.want {
			t.Errorf("%s = %q, want %q", c.name, s, c.want)
		}
	}
}

// TestCheckedArithmeticRefusesRatherThanWrapping pins that every operation past
// the engine's i128 range is an error. The engine's `checked_*` methods return
// None there, and answering anything at all would be a value the engine could
// never produce.
func TestCheckedArithmeticRefusesRatherThanWrapping(t *testing.T) {
	i := func(s string) Value {
		x, _ := new(big.Int).SetString(s, 10)
		return FromBigInt(x)
	}
	i128Max := i("170141183460469231731687303715884105727")
	i128Min, err := i128Max.Neg()
	if err != nil {
		t.Fatal(err)
	}
	i128Min, err = i128Min.Sub(FromInt(1))
	if err != nil {
		t.Fatal(err)
	}
	if s := i128Min.String(); s != "-170141183460469231731687303715884105728" {
		t.Fatalf("i128::MIN = %q", s)
	}

	cases := []struct {
		name string
		got  func() (Value, error)
	}{
		{"i128::MAX + 1", func() (Value, error) { return i128Max.Add(FromInt(1)) }},
		{"i128::MAX * 2", func() (Value, error) { return i128Max.Mul(FromInt(2)) }},
		{"i128::MIN - 1", func() (Value, error) { return i128Min.Sub(FromInt(1)) }},
		{"-i128::MIN", func() (Value, error) { return i128Min.Neg() }},
		{"i128::MIN // -1", func() (Value, error) { return i128Min.FloorDiv(FromInt(-1)) }},
		{"i128::MIN % -1", func() (Value, error) { return i128Min.Rem(FromInt(-1)) }},
		{"1 // 0", func() (Value, error) { return FromInt(1).FloorDiv(FromInt(0)) }},
		{"1 % 0", func() (Value, error) { return FromInt(1).Rem(FromInt(0)) }},
		{"2 ** 127", func() (Value, error) { return FromInt(2).Pow(FromInt(127)) }},
		{"2 ** -1", func() (Value, error) { return FromInt(2).Pow(FromInt(-1)) }},
		{"2 ** (u32::MAX+1)", func() (Value, error) { return FromInt(2).Pow(FromInt(1 << 32)) }},
		{"-true", func() (Value, error) { return FromBool(true).Neg() }},
		// A u128 past i128::MAX does not convert, so pairing it with anything
		// that is not also a u128 is an impossible operation.
		{"u128::MAX + 1", func() (Value, error) {
			return i("340282366920938463463374607431768211455").Add(FromInt(1))
		}},
	}
	for _, c := range cases {
		if got, err := c.got(); err == nil {
			t.Errorf("%s = %q, want an error", c.name, got.String())
		}
	}
}

// TestU128PairCoercesByWrapping pins the one coerce arm that is not equivalent
// to the generic i128::try_from path.
func TestU128PairCoercesByWrapping(t *testing.T) {
	x, _ := new(big.Int).SetString("340282366920938463463374607431768211455", 10)
	u128Max := FromBigInt(x)
	got, err := u128Max.Add(u128Max)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	// Both operands cast to i128 as -1.
	if s := got.String(); s != "-2" {
		t.Errorf("u128::MAX + u128::MAX = %q, want %q", s, "-2")
	}
}

// TestComparisonIsExact pins that comparison no longer routes integers through
// float64, and that the lossless coercion refuses a pair it cannot compare
// exactly.
func TestComparisonIsExact(t *testing.T) {
	i := func(s string) Value {
		x, _ := new(big.Int).SetString(s, 10)
		return FromBigInt(x)
	}
	if i("9007199254740993").Equal(i("9007199254740992")) {
		t.Error("2^53+1 == 2^53 must be false")
	}
	if i("9007199254740993").Equal(FromFloat(9007199254740992.0)) {
		t.Error("2^53+1 == 2^53 as a float must be false: the integer does not survive the round trip")
	}
	if c, ok := i("9007199254740992").Compare(i("9007199254740993")); !ok || c != -1 {
		t.Errorf("2^53 < 2^53+1 = (%d, %v), want (-1, true)", c, ok)
	}
	// Ordering an integer that cannot be coerced losslessly against a float
	// PANICS, because the engine does: its Ord handles the failed coercion by
	// unwrapping self.as_object(), which for two numbers is a None. Equality
	// reaches the same failed coercion and answers false, because the engine's
	// PartialEq handles it safely.
	func() {
		defer func() {
			switch r := recover().(type) {
			case nil:
				t.Error("ordering an integer past 2^53 against a float must panic, as the engine does")
			case UncomparableNumbers:
				if r.Left.String() != "9007199254740993" || r.Right.String() != "1.5" {
					t.Errorf("panic carries the wrong operands: %v", r)
				}
			default:
				t.Errorf("panicked with %T, want UncomparableNumbers", r)
			}
		}()
		i("9007199254740993").Compare(FromFloat(1.5))
	}()
	if i("9007199254740993").Equal(FromFloat(1.5)) {
		t.Error("equality over the same pair must answer false rather than panicking")
	}
	// f64_total_cmp: a total order, unlike Go's < and >.
	if c, ok := FromFloat(math.Copysign(0, -1)).Compare(FromFloat(0)); !ok || c != -1 {
		t.Errorf("-0.0 < 0.0 = (%d, %v), want (-1, true)", c, ok)
	}
	if !FromFloat(math.Copysign(0, -1)).Equal(FromFloat(0)) {
		t.Error("-0.0 == 0.0 must still be true")
	}
	if c, ok := FromFloat(1).Compare(FromFloat(math.NaN())); !ok || c != -1 {
		t.Errorf("1.0 vs NaN = (%d, %v), want (-1, true): NaN sorts above every number", c, ok)
	}
}

// TestFloatRenderingMatchesRustDisplay pins the shortest round-tripping
// positional form the engine uses.
func TestFloatRenderingMatchesRustDisplay(t *testing.T) {
	// Written through variables: Go folds untyped constant arithmetic at
	// arbitrary precision, so `0.1 + 0.2` as a literal would be exactly 0.3 and
	// would not exercise the case this row is here for.
	tenth, fifth := 0.1, 0.2
	cases := []struct {
		in   float64
		want string
	}{
		{1.0 / 3.0, "0.3333333333333333"},
		{2.0, "2.0"},
		{tenth + fifth, "0.30000000000000004"},
		{1e-10, "0.0000000001"},
		{9223372036854775808.0, "9223372036854776000.0"},
		{math.NaN(), "NaN"},
		{math.Inf(1), "inf"},
		{math.Inf(-1), "-inf"},
		{math.Copysign(0, -1), "-0.0"},
	}
	for _, c := range cases {
		if got := FromFloat(c.in).String(); got != c.want {
			t.Errorf("FromFloat(%v).String() = %q, want %q", c.in, got, c.want)
		}
	}
	if got := FromFloat(1e300).String(); len(got) != 303 || got[:2] != "10" {
		t.Errorf("FromFloat(1e300).String() has length %d, want a 301-digit positional form plus \".0\"", len(got))
	}
}

// TestInt64FastPathAgreesWithTheWideForm is the guard the corpus cannot be.
//
// `Add`, `Sub`, `Mul`, `FloorDiv` and `Rem` short-circuit when both operands
// already fit int64, so the hot path is not the one the exact model describes.
// A fast path that is wrong for some operand pair the corpus does not contain
// would pass the differential and still be a bug, so both paths are computed
// for a large sample and required to agree — including a deliberate sweep of the
// boundary values where int64 arithmetic overflows.
func TestInt64FastPathAgreesWithTheWideForm(t *testing.T) {
	interesting := []int64{
		0, 1, -1, 2, -2, 3, -3, 7, -7,
		math.MaxInt64, math.MinInt64, math.MaxInt64 - 1, math.MinInt64 + 1,
		1 << 31, -(1 << 31), 1 << 32, 1 << 53, -(1 << 53), (1 << 53) + 1,
		1 << 62, -(1 << 62), 4611686018427387904, 9007199254740993,
	}
	rng := rand.New(rand.NewSource(20260728))
	operands := append([]int64(nil), interesting...)
	for i := 0; i < 400; i++ {
		operands = append(operands, rng.Int63()-(1<<62), rng.Int63n(1000)-500)
	}

	ops := []struct {
		name string
		fast func(a, b Value) (Value, error)
		// slow forces the wide path by widening one operand past int64 and
		// back, which cannot take the c.small branch.
		exact func(a, b *big.Int) (*big.Int, bool)
	}{
		{"+", Value.Add, func(a, b *big.Int) (*big.Int, bool) {
			r := new(big.Int).Add(a, b)
			return r, inI128(r)
		}},
		{"-", Value.Sub, func(a, b *big.Int) (*big.Int, bool) {
			r := new(big.Int).Sub(a, b)
			return r, inI128(r)
		}},
		{"*", Value.Mul, func(a, b *big.Int) (*big.Int, bool) {
			r := new(big.Int).Mul(a, b)
			return r, inI128(r)
		}},
		{"//", Value.FloorDiv, func(a, b *big.Int) (*big.Int, bool) {
			if b.Sign() == 0 {
				return nil, false
			}
			r := new(big.Int).Div(a, b)
			return r, inI128(r)
		}},
		{"%", Value.Rem, func(a, b *big.Int) (*big.Int, bool) {
			if b.Sign() == 0 || (a.Cmp(bigI128Min) == 0 && b.Cmp(bigNegOne) == 0) {
				return nil, false
			}
			return new(big.Int).Mod(a, b), true
		}},
	}

	checked := 0
	for _, op := range ops {
		for _, x := range operands {
			for _, y := range interesting {
				got, err := op.fast(FromInt(x), FromInt(y))
				want, ok := op.exact(big.NewInt(x), big.NewInt(y))
				switch {
				case ok && err != nil:
					t.Fatalf("%d %s %d: got error %v, want %s", x, op.name, y, err, want)
				case !ok && err == nil:
					t.Fatalf("%d %s %d: got %s, want an error", x, op.name, y, got.String())
				case ok && got.String() != want.String():
					t.Fatalf("%d %s %d = %s, want %s", x, op.name, y, got.String(), want)
				}
				checked++
			}
		}
	}
	// Guard against the loops silently becoming empty.
	if checked < 50000 {
		t.Errorf("only %d operand pairs checked", checked)
	}
}

// TestU64PayloadIsInvisibleExceptToLosslessFloatCoercion is the completeness
// check for the U64 repr.
//
// ValueRepr::U64 and ValueRepr::I64 differ in the engine in exactly one place:
// `as_f64`'s lossless round trip casts back to the operand's own type, and the
// two saturate differently. Everywhere else they must be indistinguishable —
// and "everywhere else" is a dozen payload type switches spread over three
// packages, so a missed one is the obvious failure mode of carrying the tag as
// a payload type at all.
//
// Rather than trusting the grep that found those switches, this requires a U64
// and an I64 of the same magnitude to agree through every exported operation,
// against a probe set chosen to reach each of them.
func TestU64PayloadIsInvisibleExceptToLosslessFloatCoercion(t *testing.T) {
	magnitudes := []int64{
		0, 1, 2, 7, 42, 1 << 31, 1 << 52, 1<<53 - 1, 1 << 53, 1<<53 + 1,
		math.MaxInt64 - 512, math.MaxInt64 - 1, math.MaxInt64,
	}
	probes := []Value{
		FromInt(0), FromInt(1), FromInt(-1), FromInt(2), FromInt(math.MaxInt64),
		FromFloat(0), FromFloat(1.5), FromFloat(2), FromBool(true),
		None(), Undefined(),
	}
	// A string or a sequence turns `*` into repetition, whose cost is the
	// magnitude, so those probes are only run where the magnitude is small.
	// They are here for `+` and for the error paths, not to allocate exabytes.
	repeatable := []Value{FromString("x"), FromSlice([]Value{FromInt(1)})}

	for _, m := range magnitudes {
		u, i := FromUint64(uint64(m)), FromInt(m)
		if _, isU64 := u.data.(u64Value); !isU64 {
			t.Fatalf("FromUint64(%d) is not a U64 payload", m)
		}

		// Everything the value can be asked about itself.
		type probe struct {
			name string
			got  func(Value) any
		}
		selfProbes := []probe{
			{"Kind", func(v Value) any { return v.Kind() }},
			{"String", func(v Value) any { return v.String() }},
			{"Repr", func(v Value) any { return v.Repr() }},
			{"IsTrue", func(v Value) any { return v.IsTrue() }},
			{"IsActualInt", func(v Value) any { return v.IsActualInt() }},
			{"IsActualFloat", func(v Value) any { return v.IsActualFloat() }},
			{"IsNone", func(v Value) any { return v.IsNone() }},
			{"IsUndefined", func(v Value) any { return v.IsUndefined() }},
			{"IsSafe", func(v Value) any { return v.IsSafe() }},
			{"Raw", func(v Value) any { return v.Raw() }},
			{"AsInt", func(v Value) any { n, ok := v.AsInt(); return [2]any{n, ok} }},
			{"AsFloat", func(v Value) any { f, ok := v.AsFloat(); return [2]any{f, ok} }},
			{"AsBigInt", func(v Value) any {
				b, ok := v.AsBigInt()
				if !ok {
					return "no"
				}
				return b.String()
			}},
			{"AsString", func(v Value) any { s, ok := v.AsString(); return [2]any{s, ok} }},
			{"AsBool", func(v Value) any { b, ok := v.AsBool(); return [2]any{b, ok} }},
			{"Len", func(v Value) any { n, ok := v.Len(); return [2]any{n, ok} }},
			{"Clone.String", func(v Value) any { return v.Clone().String() }},
		}
		for _, p := range selfProbes {
			if a, b := p.got(u), p.got(i); a != b {
				t.Errorf("%d: U64.%s = %v, I64.%s = %v", m, p.name, a, p.name, b)
			}
		}

		// Everything the value can be asked about a partner. Arithmetic
		// coerces lossily, so it must agree for every probe; equality and
		// ordering coerce losslessly, which is the one documented difference.
		type binop struct {
			name string
			got  func(a, b Value) (Value, error)
		}
		for _, op := range []binop{
			{"Add", Value.Add}, {"Sub", Value.Sub}, {"Mul", Value.Mul},
			{"Div", Value.Div}, {"FloorDiv", Value.FloorDiv}, {"Rem", Value.Rem},
			{"Pow", Value.Pow},
		} {
			partners := probes
			if m < 64 {
				partners = append(append([]Value(nil), probes...), repeatable...)
			}
			for _, p := range partners {
				for _, flip := range []bool{false, true} {
					var ru, ri Value
					var eu, ei error
					if flip {
						ru, eu = op.got(p, u)
						ri, ei = op.got(p, i)
					} else {
						ru, eu = op.got(u, p)
						ri, ei = op.got(i, p)
					}
					if (eu == nil) != (ei == nil) {
						t.Errorf("%d %s %v (flip=%v): U64 err=%v, I64 err=%v", m, op.name, p.Repr(), flip, eu, ei)
						continue
					}
					if eu == nil && ru.String() != ri.String() {
						t.Errorf("%d %s %v (flip=%v): U64 = %s, I64 = %s", m, op.name, p.Repr(), flip, ru, ri)
					}
				}
			}
			if r, err := u.Neg(); err != nil {
				t.Errorf("%d: U64 Neg errored: %v", m, err)
			} else if ri, _ := i.Neg(); r.String() != ri.String() {
				t.Errorf("%d: U64 Neg = %s, I64 Neg = %s", m, r, ri)
			}
		}

		// Equality agrees for every probe EXCEPT a float whose round trip
		// differs, which is exactly the documented case.
		for _, p := range append(append([]Value(nil), probes...), repeatable...) {
			if u.Equal(p) != i.Equal(p) || p.Equal(u) != p.Equal(i) {
				if !isTheDocumentedU64Difference(m, p) {
					t.Errorf("%d: Equal against %v differs between U64 and I64", m, p.Repr())
				}
			}
		}
	}

	// And the documented difference itself, in both directions.
	const twoPow63 = 9223372036854775808.0
	if FromUint64(math.MaxInt64).Equal(FromFloat(twoPow63)) {
		t.Error("U64(i64::MAX) == 2^63 must be false: the f64 casts back to u64 as 2^63")
	}
	if !FromInt(math.MaxInt64).Equal(FromFloat(twoPow63)) {
		t.Error("I64(i64::MAX) == 2^63 must be true: the f64 saturates back to i64::MAX")
	}
	// u64::MAX is the mirror image: its f64 image is 2^64, which saturates back
	// through u64 and therefore DOES round-trip.
	if !FromUint64(math.MaxUint64).Equal(FromFloat(18446744073709551616.0)) {
		t.Error("U64(u64::MAX) == 2^64 must be true")
	}
}

// isTheDocumentedU64Difference reports whether a U64/I64 disagreement is the one
// the payload type exists for: a magnitude whose f64 image saturates back
// differently through u64 than through i64.
func isTheDocumentedU64Difference(m int64, p Value) bool {
	if !p.IsActualFloat() {
		return false
	}
	f := float64(m)
	return satCastU64(f) != uint64(m) && satCastI64(f) == m
}

// TestAsIntIsTheEnginesConversion pins the three parts of `i64::try_from` the
// port got wrong.
func TestAsIntIsTheEnginesConversion(t *testing.T) {
	x, _ := new(big.Int).SetString("9223372036854775808", 10)
	cases := []struct {
		name string
		in   Value
		want int64
		ok   bool
	}{
		{"bool true", FromBool(true), 1, true},
		{"bool false", FromBool(false), 0, true},
		{"integral float", FromFloat(2), 2, true},
		{"fractional float", FromFloat(1.5), 0, false},
		{"2^63 as a float saturates", FromFloat(9223372036854775808.0), math.MaxInt64, true},
		{"2^64 as a float refuses", FromFloat(18446744073709551616.0), 0, false},
		{"an integer past i64 refuses", FromBigInt(x), 0, false},
		{"a string refuses", FromString("1"), 0, false},
	}
	for _, c := range cases {
		got, ok := c.in.AsInt()
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("%s: AsInt() = (%d, %v), want (%d, %v)", c.name, got, ok, c.want, c.ok)
		}
	}
	// AsBigInt is the same conversion at i128 width, which is what a value past
	// int64 needs.
	if b, ok := FromBigInt(x).AsBigInt(); !ok || b.String() != "9223372036854775808" {
		t.Errorf("AsBigInt() over an integer past i64 = (%v, %v)", b, ok)
	}
}

// TestAsUsizeIsTheEnginesConversionAtItsOwnWidth pins the arm the whole usize
// finding was about: the DECLARED type bounds the conversion, so `usize` and
// `i64` part company over the entire upper half of u64.
func TestAsUsizeIsTheEnginesConversionAtItsOwnWidth(t *testing.T) {
	big2p63, _ := new(big.Int).SetString("9223372036854775808", 10)
	u64max, _ := new(big.Int).SetString("18446744073709551615", 10)
	past, _ := new(big.Int).SetString("18446744073709551616", 10)
	cases := []struct {
		name string
		in   Value
		want uint64
		ok   bool
	}{
		{"bool true", FromBool(true), 1, true},
		{"bool false", FromBool(false), 0, true},
		{"zero", FromInt(0), 0, true},
		{"i64::MAX", FromInt(math.MaxInt64), math.MaxInt64, true},
		{"a negative refuses", FromInt(-1), 0, false},
		{"integral float", FromFloat(2), 2, true},
		{"fractional float refuses", FromFloat(1.5), 0, false},
		{"negative float refuses", FromFloat(-1), 0, false},
		{"2^63 as a float saturates onto i64::MAX", FromFloat(9223372036854775808.0), math.MaxInt64, true},
		{"2^63 CONVERTS, where AsInt refuses it", FromBigInt(big2p63), 9223372036854775808, true},
		{"u64::MAX converts", FromBigInt(u64max), math.MaxUint64, true},
		{"past u64::MAX refuses", FromBigInt(past), 0, false},
		{"a string refuses", FromString("1"), 0, false},
	}
	for _, c := range cases {
		got, ok := c.in.AsUsize()
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("%s: AsUsize() = (%d, %v), want (%d, %v)", c.name, got, ok, c.want, c.ok)
		}
	}
	// The whole point: the two conversions disagree over that range, and the
	// engine's ArgTypes pick between them by the parameter's declared type.
	if _, ok := FromBigInt(big2p63).AsInt(); ok {
		t.Error("AsInt accepted 2^63; it is an i64 conversion")
	}
}
