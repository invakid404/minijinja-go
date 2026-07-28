package value

import (
	"math"
	"testing"
)

// TestComparisonCoercionIsNotLossy pins the rule that separates comparison from
// arithmetic: comparison coerces with lossy = false, so an integer that cannot
// round-trip through f64 does not compare numerically with a float at all.
func TestComparisonCoercionIsNotLossy(t *testing.T) {
	const twoPow53 = 9007199254740992
	big := FromInt(twoPow53 + 1)
	asFloat := FromFloat(twoPow53)

	if big.Equal(asFloat) {
		t.Error("2^53+1 compared equal to 2^53 as a float; the coercion was lossy")
	}
	if asFloat.Equal(big) {
		t.Error("the same comparison in the other operand order was lossy")
	}
	// An integer that does round-trip still compares.
	if !FromInt(2).Equal(FromFloat(2)) {
		t.Error("2 did not compare equal to 2.0")
	}
}

// TestNoNumberStringCoercion pins that neither equality nor ordering parses
// strings: `1 == '1'` is false and `1 < 'a'` is decided by kind order.
func TestNoNumberStringCoercion(t *testing.T) {
	if FromInt(1).Equal(FromString("1")) || FromString("1").Equal(FromInt(1)) {
		t.Error("a number compared equal to its text")
	}
	if got, ok := FromInt(1).Compare(FromString("a")); !ok || sign(got) != -1 {
		t.Errorf("1 < 'a' = %d (ok=%v), want less by kind order", got, ok)
	}
	if got, ok := FromString("a").Compare(FromInt(1)); !ok || sign(got) != 1 {
		t.Errorf("'a' > 1 = %d (ok=%v), want greater by kind order", got, ok)
	}
}

// TestFloatOrderingIsTotal pins the two visible consequences of ordering floats
// with a total order rather than with `<`.
func TestFloatOrderingIsTotal(t *testing.T) {
	nan := FromFloat(math.NaN())
	one := FromFloat(1)

	if got, ok := nan.Compare(one); !ok || sign(got) != 1 {
		t.Errorf("NaN vs 1 = %d (ok=%v), want greater: NaN sorts above every number", got, ok)
	}
	if got, ok := one.Compare(nan); !ok || sign(got) != -1 {
		t.Errorf("1 vs NaN = %d (ok=%v), want less", got, ok)
	}
	if got, ok := nan.Compare(nan); !ok || sign(got) != 0 {
		t.Errorf("NaN vs NaN = %d (ok=%v), want equal for ordering", got, ok)
	}
	if nan.Equal(nan) {
		t.Error("NaN compared equal to itself; equality is still IEEE")
	}

	negZero, zero := FromFloat(math.Copysign(0, -1)), FromFloat(0)
	if got, ok := negZero.Compare(zero); !ok || sign(got) != -1 {
		t.Errorf("-0.0 vs 0.0 = %d (ok=%v), want less under a total order", got, ok)
	}
	if !negZero.Equal(zero) {
		t.Error("-0.0 did not compare equal to 0.0; equality is still IEEE")
	}
}

// TestNaNIsTruthy pins truthiness as the engine defines it: `x != 0.0`.
func TestNaNIsTruthy(t *testing.T) {
	if !FromFloat(math.NaN()).IsTrue() {
		t.Error("NaN was falsy")
	}
	if FromFloat(0).IsTrue() || FromFloat(math.Copysign(0, -1)).IsTrue() {
		t.Error("zero was truthy")
	}
}

// TestSubscriptUsesPrimitiveConversion pins the container paths the conversion
// feeds: sequences, iterators and strings all index by a bool.
func TestSubscriptUsesPrimitiveConversion(t *testing.T) {
	xs := FromSlice([]Value{FromInt(1), FromInt(2), FromInt(3)})
	iter := FromIterator(NewIterator("range", []Value{FromInt(0), FromInt(1), FromInt(2)}))
	s := FromString("abc")

	for _, c := range []struct {
		name string
		got  Value
		want Value
	}{
		{"sequence[true]", xs.GetItem(FromBool(true)), FromInt(2)},
		{"sequence[false]", xs.GetItem(FromBool(false)), FromInt(1)},
		{"iterator[true]", iter.GetItem(FromBool(true)), FromInt(1)},
		{"string[true]", s.GetItem(FromBool(true)), FromString("b")},
		{"sequence[-1.0]", xs.GetItem(FromFloat(-1)), FromInt(3)},
	} {
		t.Run(c.name, func(t *testing.T) {
			if !c.got.Equal(c.want) {
				t.Errorf("= %s, want %s", c.got.Repr(), c.want.Repr())
			}
		})
	}

	for _, c := range []struct {
		name string
		got  Value
	}{
		{"sequence[1.5]", xs.GetItem(FromFloat(1.5))},
		{"sequence[none]", xs.GetItem(None())},
		{"sequence['0']", xs.GetItem(FromString("0"))},
	} {
		t.Run(c.name, func(t *testing.T) {
			if !c.got.IsUndefined() {
				t.Errorf("= %s, want undefined", c.got.Repr())
			}
		})
	}
}

// TestRepetitionCountUsesPrimitiveConversion pins the repetition branches, which
// the engine resolves before any numeric coercion and which therefore convert
// their count the same way a subscript does.
func TestRepetitionCountUsesPrimitiveConversion(t *testing.T) {
	got, err := FromString("a").Mul(FromBool(true))
	if err != nil || got.String() != "a" {
		t.Errorf("'a' * true = %q (err=%v), want \"a\"", got.String(), err)
	}
	got, err = FromBool(true).Mul(FromString("a"))
	if err != nil || got.String() != "a" {
		t.Errorf("true * 'a' = %q (err=%v), want \"a\"", got.String(), err)
	}
	got, err = FromSlice([]Value{FromInt(1), FromInt(2)}).Mul(FromBool(true))
	if err != nil || got.String() != "[1, 2]" {
		t.Errorf("[1, 2] * true = %q (err=%v), want [1, 2]", got.String(), err)
	}
	if got.Kind() != KindIterable {
		t.Errorf("a repeated sequence has kind %s, want iterable", got.Kind())
	}
	if _, err := FromString("a").Mul(FromInt(-1)); err == nil {
		t.Error("'a' * -1 did not report an error")
	}
	if _, err := FromString("a").Mul(FromFloat(2.5)); err == nil {
		t.Error("'a' * 2.5 did not report an error")
	}
}

// TestOrderingIsTotalForPayloadlessKinds pins that ordering answers rather than
// refusing for kinds with nothing to compare. Refusing made the VM raise on
// `none < none`.
func TestOrderingIsTotalForPayloadlessKinds(t *testing.T) {
	for _, c := range []struct {
		name string
		a, b Value
	}{
		{"none", None(), None()},
		{"undefined", Undefined(), Undefined()},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, ok := c.a.Compare(c.b)
			if !ok {
				t.Fatal("Compare reported incomparable")
			}
			if got != 0 {
				t.Errorf("Compare = %d, want 0", got)
			}
		})
	}
}

// TestKindOrdering pins the kind ranks, which are observable through `<` on
// mixed types and are the declaration order of the Rust engine's ValueKind.
func TestKindOrdering(t *testing.T) {
	ordered := []Value{
		Undefined(),
		None(),
		FromBool(true),
		FromInt(1),
		FromString("a"),
		FromBytes([]byte("a")),
		FromSlice([]Value{FromInt(1)}),
		FromOrderedMap(OrderedMapFromPairs([]string{"a"}, []Value{FromInt(1)})),
		FromIterator(NewIterator("x", []Value{FromInt(1)})),
		FromObject(&silentObject{label: "plain"}),
	}
	for i := 0; i < len(ordered); i++ {
		for j := i + 1; j < len(ordered); j++ {
			got, ok := ordered[i].Compare(ordered[j])
			if !ok {
				t.Errorf("%s vs %s: incomparable", ordered[i].Kind(), ordered[j].Kind())
				continue
			}
			if sign(got) != -1 {
				t.Errorf("%s vs %s = %d, want less", ordered[i].Kind(), ordered[j].Kind(), got)
			}
		}
	}
}

// TestContainmentRules pins the asymmetry between a string container, which
// stringifies the needle, and a sequence, which compares by equality.
func TestContainmentRules(t *testing.T) {
	if got, err := FromString("1").TryContains(FromInt(1)); err != nil || !got {
		t.Errorf("1 in '1' = %v (err=%v), want true", got, err)
	}
	if got, err := FromString("none").TryContains(None()); err != nil || !got {
		t.Errorf("none in 'none' = %v (err=%v), want true", got, err)
	}
	if got, err := FromSlice([]Value{FromInt(1)}).TryContains(FromString("1")); err != nil || got {
		t.Errorf("'1' in [1] = %v (err=%v), want false: a sequence does not stringify", got, err)
	}
	if got, err := FromSlice([]Value{FromInt(1)}).TryContains(FromBool(true)); err != nil || !got {
		t.Errorf("true in [1] = %v (err=%v), want true", got, err)
	}
	if _, err := FromInt(5).TryContains(FromInt(1)); err == nil {
		t.Error("1 in 5 did not report an error; a non-container must not answer false")
	}
	if _, err := None().TryContains(FromInt(1)); err == nil {
		t.Error("1 in none did not report an error")
	}
	if got, err := Undefined().TryContains(FromInt(1)); err != nil || got {
		t.Errorf("1 in undefined = %v (err=%v), want false without an error", got, err)
	}
}

// TestSequenceAndIterableEqualButNotEquallyOrdered pins a pair of rules that
// look contradictory and are both real: a lazy iterable and a sequence with the
// same items are *equal*, but ordering still separates them by kind, because the
// hook and the kind comparison run before the container comparison. It is what
// keeps a slice or a range comparable with a list while `range(2) > [0, 1]`
// stays true.
func TestSequenceAndIterableEqualButNotEquallyOrdered(t *testing.T) {
	seq := FromSlice([]Value{FromInt(0), FromInt(1)})
	iter := FromIterator(NewIterator("range", []Value{FromInt(0), FromInt(1)}))

	if !seq.Equal(iter) || !iter.Equal(seq) {
		t.Error("a sequence and an iterable with the same items compared unequal")
	}
	if got, ok := iter.Compare(seq); !ok || sign(got) != 1 {
		t.Errorf("iterable vs sequence = %d (ok=%v), want greater by kind order", got, ok)
	}
	if got, ok := seq.Compare(iter); !ok || sign(got) != -1 {
		t.Errorf("sequence vs iterable = %d (ok=%v), want less by kind order", got, ok)
	}
}

// TestEmptyIterableIsNotNil pins that Iter distinguishes "empty" from "not
// iterable"; conflating them made an empty sequence render as a list where a
// caller expected no items.
func TestEmptyIterableIsNotNil(t *testing.T) {
	for _, c := range []struct {
		name string
		v    Value
	}{
		{"nil slice", FromSlice(nil)},
		{"empty slice", FromSlice([]Value{})},
		{"empty iterator", FromIterator(NewIterator("x", nil))},
		{"empty ordered map", FromOrderedMap(NewOrderedMap(0))},
		{"empty map", FromMap(map[string]Value{})},
		{"empty string", FromString("")},
	} {
		t.Run(c.name, func(t *testing.T) {
			if items := c.v.Iter(); items == nil {
				t.Error("Iter returned nil for an iterable value")
			}
		})
	}
	if items := FromInt(1).Iter(); items != nil {
		t.Error("Iter returned non-nil for a number")
	}
}
