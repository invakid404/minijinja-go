package value

import "testing"

func orderedFixture() Value {
	m := NewOrderedMap(3)
	m.Set("b", FromInt(1))
	m.Set("a", FromInt(2))
	m.Set("c", FromInt(3))
	return FromOrderedMap(m)
}

func TestOrderedMapPreservesInsertionOrder(t *testing.T) {
	v := orderedFixture()

	keys, ok := v.MapKeys()
	if !ok {
		t.Fatal("MapKeys reported no keys")
	}
	if got, want := keys, []string{"b", "a", "c"}; !equalStrings(got, want) {
		t.Errorf("MapKeys = %v, want %v", got, want)
	}

	items := v.Iter()
	var iterated []string
	for _, item := range items {
		s, _ := item.AsString()
		iterated = append(iterated, s)
	}
	if !equalStrings(iterated, []string{"b", "a", "c"}) {
		t.Errorf("Iter = %v, want [b a c]", iterated)
	}

	// Display and debug are the same rendering, so a nested map does not
	// disagree with the same map at the top level.
	const want = `{"b": 1, "a": 2, "c": 3}`
	if got := v.String(); got != want {
		t.Errorf("String = %s, want %s", got, want)
	}
	if got := v.Repr(); got != want {
		t.Errorf("Repr = %s, want %s", got, want)
	}
	if got := FromSlice([]Value{v}).String(); got != "["+want+"]" {
		t.Errorf("nested String = %s, want [%s]", got, want)
	}
}

func TestOrderedMapRepeatedKeyKeepsPosition(t *testing.T) {
	m := NewOrderedMap(2)
	m.Set("b", FromInt(1))
	m.Set("a", FromInt(2))
	m.Set("b", FromInt(3))

	if got, want := m.Keys(), []string{"b", "a"}; !equalStrings(got, want) {
		t.Errorf("Keys = %v, want %v: a repeated key must keep its position", got, want)
	}
	if got := FromOrderedMap(m).String(); got != `{"b": 3, "a": 2}` {
		t.Errorf("String = %s, want {\"b\": 3, \"a\": 2}", got)
	}
}

// TestOrderedMapEqualityIgnoresOrder pins that preserving order for rendering
// does not make two equal mappings unequal.
func TestOrderedMapEqualityIgnoresOrder(t *testing.T) {
	a := NewOrderedMap(2)
	a.Set("b", FromInt(1))
	a.Set("a", FromInt(2))
	b := NewOrderedMap(2)
	b.Set("a", FromInt(2))
	b.Set("b", FromInt(1))

	if !FromOrderedMap(a).Equal(FromOrderedMap(b)) {
		t.Error("maps with the same entries in a different order compared unequal")
	}
	if !FromOrderedMap(a).Equal(FromMap(map[string]Value{"a": FromInt(2), "b": FromInt(1)})) {
		t.Error("an ordered map was not equal to an unordered map with the same entries")
	}
}

// TestOrderedMapOrderingUsesIterationOrder pins the flip side: ordering compares
// pairs in iteration order, so it does depend on insertion order. The Rust engine
// documents this and accepts it rather than paying to sort.
func TestOrderedMapOrderingUsesIterationOrder(t *testing.T) {
	a := NewOrderedMap(2)
	a.Set("b", FromInt(1))
	a.Set("a", FromInt(2))
	b := NewOrderedMap(2)
	b.Set("a", FromInt(2))
	b.Set("b", FromInt(1))

	got, ok := FromOrderedMap(a).Compare(FromOrderedMap(b))
	if !ok {
		t.Fatal("two mappings were incomparable")
	}
	if sign(got) != 1 {
		t.Errorf("Compare = %d, want greater (first pair \"b\" > \"a\")", got)
	}
}

// TestUnorderedMapIsDeterministic pins the fallback: a value built from a Go map
// has no order to preserve, and must not render in random order.
func TestUnorderedMapIsDeterministic(t *testing.T) {
	v := FromMap(map[string]Value{"c": FromInt(3), "a": FromInt(1), "b": FromInt(2)})
	const want = `{"a": 1, "b": 2, "c": 3}`
	for i := 0; i < 25; i++ {
		if got := v.String(); got != want {
			t.Fatalf("String = %s, want %s", got, want)
		}
	}
}

// TestOrderedMapKeySpelling pins that a key remembers whether it was written as
// a string or as another value, because the Rust engine keys maps by value and
// renders `{1: "a"}` differently from `{"1": "a"}`.
func TestOrderedMapKeySpelling(t *testing.T) {
	intKey := NewOrderedMap(1)
	intKey.SetKeyValue(FromInt(1), FromString("a"))
	if got, want := FromOrderedMap(intKey).String(), `{1: "a"}`; got != want {
		t.Errorf("integer key rendered %s, want %s", got, want)
	}

	stringKey := NewOrderedMap(1)
	stringKey.SetKeyValue(FromString("1"), FromString("a"))
	if got, want := FromOrderedMap(stringKey).String(), `{"1": "a"}`; got != want {
		t.Errorf("string key rendered %s, want %s", got, want)
	}
	if got, want := FromSlice([]Value{FromOrderedMap(stringKey)}).String(), `[{"1": "a"}]`; got != want {
		t.Errorf("nested string key rendered %s, want %s", got, want)
	}
}

func TestOrderedMapLookupAndLength(t *testing.T) {
	v := orderedFixture()
	if got, _ := v.Len(); got != 3 {
		t.Errorf("Len = %d, want 3", got)
	}
	if !v.IsTrue() {
		t.Error("a non-empty mapping was falsy")
	}
	if FromOrderedMap(NewOrderedMap(0)).IsTrue() {
		t.Error("an empty mapping was truthy")
	}
	if got := v.GetItem(FromString("a")); !got.Equal(FromInt(2)) {
		t.Errorf("GetItem(a) = %s, want 2", got.Repr())
	}
	if got := v.GetAttr("c"); !got.Equal(FromInt(3)) {
		t.Errorf("GetAttr(c) = %s, want 3", got.Repr())
	}
	if got := v.GetItem(FromString("zz")); !got.IsUndefined() {
		t.Errorf("a missing key returned %s, want undefined", got.Repr())
	}
	if got, err := v.TryContains(FromString("b")); err != nil || !got {
		t.Errorf("'b' in m = %v (err=%v), want true", got, err)
	}
	if got, err := v.TryContains(FromInt(1)); err != nil || got {
		t.Errorf("1 in m = %v (err=%v), want false: values are not members", got, err)
	}
}

func TestOrderedMapCloneIsIndependent(t *testing.T) {
	m := NewOrderedMap(1)
	m.SetKeyValue(FromInt(1), FromString("a"))
	clone := m.Clone()
	clone.Set("b", FromInt(2))

	if m.Len() != 1 {
		t.Errorf("the original grew to %d entries", m.Len())
	}
	if got, want := FromOrderedMap(clone).String(), `{1: "a", "b": 2}`; got != want {
		t.Errorf("clone rendered %s, want %s", got, want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
