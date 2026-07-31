package value

import (
	"strings"
	"testing"
)

// A mapping that cannot enumerate its pairs is not an empty mapping.
//
// The engine's map comparison is written against `try_iter_pairs()`, which is
// None for an object whose `enumerate` is `Enumerator::NonEnumerable`
// (value/object.rs:361-396). An `ObjectRepr::Map` object that returns that — the
// shape BAML gives an enum member and an enum namespace — is a map by
// REPRESENTATION with no pairs to walk, and the engine's equality
// (value/mod.rs:533-559) and ordering (value/mod.rs:648-661) both behave
// differently for it than for `{}`.
//
// The fork's counterpart of "no pair iterator" is [Value.MapKeys] reporting
// `ok == false`, which it does for an object that reports [ObjectReprMap] and
// implements neither [MapObject] nor [MapGetter]. These tests are written
// against exactly that generic shape: no host profile is involved, and the same
// answers are owed to any host whose map object is opaque.

// opaqueMap is the generic non-enumerable mapping. It reports ObjectReprMap and
// deliberately implements neither MapObject nor MapGetter, so it has no
// enumerable pairs — the Go spelling of `Enumerator::NonEnumerable`.
//
// It carries a display string because BAML's enum objects override `render`;
// nothing about the comparison rules depends on it.
type opaqueMap struct{ display string }

var (
	_ Object           = (*opaqueMap)(nil)
	_ ObjectWithRepr   = (*opaqueMap)(nil)
	_ ObjectWithString = (*opaqueMap)(nil)
)

func (o *opaqueMap) GetAttr(string) Value   { return Undefined() }
func (o *opaqueMap) ObjectRepr() ObjectRepr { return ObjectReprMap }
func (o *opaqueMap) ObjectString() string   { return o.display }

// opaqueCmpMap is an opaque mapping that ALSO answers the generic comparison
// hook, the way a host enum member compares by its canonical value. It answers
// for strings and for another member, and declines everything else — including
// the namespace object — which is what makes the fallback below reachable at
// all.
type opaqueCmpMap struct {
	canonical string
	display   string
}

var (
	_ Object             = (*opaqueCmpMap)(nil)
	_ ObjectWithRepr     = (*opaqueCmpMap)(nil)
	_ ObjectWithString   = (*opaqueCmpMap)(nil)
	_ ObjectWithValueCmp = (*opaqueCmpMap)(nil)
)

func (o *opaqueCmpMap) GetAttr(string) Value   { return Undefined() }
func (o *opaqueCmpMap) ObjectRepr() ObjectRepr { return ObjectReprMap }
func (o *opaqueCmpMap) ObjectString() string   { return o.display }

func (o *opaqueCmpMap) ValueCmp(other Value) (int, bool) {
	if s, ok := other.AsString(); ok {
		return strings.Compare(o.canonical, s), true
	}
	if obj, ok := other.AsObject(); ok {
		if other, ok := obj.(*opaqueCmpMap); ok {
			return strings.Compare(o.canonical, other.canonical), true
		}
	}
	return 0, false
}

// enumerableMap is the control: a host object that IS an enumerable mapping, so
// the same code path must keep treating it structurally.
type enumerableMap struct{ m *OrderedMap }

var (
	_ Object         = (*enumerableMap)(nil)
	_ ObjectWithRepr = (*enumerableMap)(nil)
	_ MapObject      = (*enumerableMap)(nil)
)

func (e *enumerableMap) GetAttr(name string) Value {
	v, ok := e.m.Get(name)
	if !ok {
		return Undefined()
	}
	return v
}
func (e *enumerableMap) ObjectRepr() ObjectRepr { return ObjectReprMap }
func (e *enumerableMap) Keys() []string         { return e.m.Keys() }

func emptyMap() Value { return FromOrderedMap(NewOrderedMap(0)) }

func mapWith(pairs ...string) Value {
	m := NewOrderedMap(len(pairs) / 2)
	for i := 0; i+1 < len(pairs); i += 2 {
		m.Set(pairs[i], FromString(pairs[i+1]))
	}
	return FromOrderedMap(m)
}

// TestMapKeysReportsNonEnumerable pins the seam the two operators branch on. An
// opaque map object must NOT look like a known-empty one.
func TestMapKeysReportsNonEnumerable(t *testing.T) {
	member := FromObject(&opaqueMap{display: "rouge"})

	if member.Kind() != KindMap {
		t.Fatalf("Kind() = %v, want KindMap: the object is a map by representation", member.Kind())
	}
	if keys, ok := member.MapKeys(); ok {
		t.Errorf("MapKeys() = (%v, true), want ok == false: an object with no MapObject/MapGetter has no enumerable pairs", keys)
	}

	// The control: an ordinary empty mapping enumerates, and reports so.
	keys, ok := emptyMap().MapKeys()
	if !ok {
		t.Error("MapKeys() on {} reported ok == false; an empty mapping has a known, empty pair iterator")
	}
	if len(keys) != 0 {
		t.Errorf("MapKeys() on {} = %v, want empty", keys)
	}

	// And so does an enumerable host map object, which must not be swept up
	// with the opaque one.
	if _, ok := FromObject(&enumerableMap{m: NewOrderedMap(0)}).MapKeys(); !ok {
		t.Error("MapKeys() on an empty MapObject reported ok == false")
	}
}

// TestOpaqueMapEqualityIsDirectional is the core of the fix.
//
// The engine returns false as soon as the LEFT operand has no pair iterator, and
// only reaches the length fallback — which counts a non-enumerable right side as
// zero — when the left one does. So the same pair of values answers differently
// depending on which side it is written on, and both answers are stock.
func TestOpaqueMapEqualityIsDirectional(t *testing.T) {
	member := FromObject(&opaqueCmpMap{canonical: "RED", display: "rouge"})
	namespace := FromObject(&opaqueMap{display: "Color"})
	otherMember := FromObject(&opaqueCmpMap{canonical: "BLUE", display: "bleu"})

	cases := []struct {
		name  string
		left  Value
		right Value
		want  bool
	}{
		// The failure this fixes: two unrelated opaque maps were two empty key
		// slices, so an enum member equalled its own namespace.
		{"member == namespace", member, namespace, false},
		{"namespace == member", namespace, member, false},
		{"namespace == namespace copy", namespace, FromObject(&opaqueMap{display: "Color"}), false},

		// The comparison hook declines for a namespace, so the fallback is
		// what answers. For two members it does NOT decline, and must keep
		// winning: that is why the hook still runs first, from both sides.
		{"member == other member", member, otherMember, false},
		{"member == same canonical", member, FromObject(&opaqueCmpMap{canonical: "RED", display: "red"}), true},

		// Identity still short-circuits.
		{"member == itself", member, member, true},
		{"namespace == itself", namespace, namespace, true},

		// The deliberate asymmetry against an ordinary empty map.
		{"member == {}", member, emptyMap(), false},
		{"{} == member", emptyMap(), member, true},
		{"namespace == {}", namespace, emptyMap(), false},
		{"{} == namespace", emptyMap(), namespace, true},

		// A non-empty left map cannot reach that fallback: its pair count is
		// not zero, and a non-enumerable right side counts as zero.
		{"{'a': 'b'} == member", mapWith("a", "b"), member, false},
		{"member == {'a': 'b'}", member, mapWith("a", "b"), false},

		// Ordinary structural comparison is untouched.
		{"{} == {}", emptyMap(), emptyMap(), true},
		{"{'a': 'b'} == {'a': 'b'}", mapWith("a", "b"), mapWith("a", "b"), true},
		{"{'a': 'b'} == {'a': 'c'}", mapWith("a", "b"), mapWith("a", "c"), false},
		{"{'a': 'b'} == {}", mapWith("a", "b"), emptyMap(), false},
		{"host map == {}", FromObject(&enumerableMap{m: NewOrderedMap(0)}), emptyMap(), true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.left.Equal(tc.right); got != tc.want {
				t.Errorf("Equal = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestOpaqueMapMembershipKeepsItsOrientation proves the membership answers fall
// out of the directional equality rather than out of a shortcut.
//
// `x in seq` tests `item.Equal(x)` for each item — the CONTAINER's item on the
// left. Adding a membership shortcut, or comparing the needle from the other
// side, would silently flip half of these.
func TestOpaqueMapMembershipKeepsItsOrientation(t *testing.T) {
	member := FromObject(&opaqueCmpMap{canonical: "RED", display: "rouge"})
	namespace := FromObject(&opaqueMap{display: "Color"})
	shade := FromObject(&opaqueMap{display: "Shade"})

	cases := []struct {
		name     string
		haystack Value
		needle   Value
		want     bool
	}{
		{"member in [namespace]", FromSlice([]Value{namespace}), member, false},
		{"namespace in [member]", FromSlice([]Value{member}), namespace, false},
		{"namespace in [shade]", FromSlice([]Value{shade}), namespace, false},
		{"member in [namespace, shade]", FromSlice([]Value{namespace, shade}), member, false},

		// The orientation is visible here: the item is compared on the left, so
		// `member in [{}]` asks `{} == member`, which is true, while
		// `{} in [member]` asks `member == {}`, which is false.
		{"member in [{}]", FromSlice([]Value{emptyMap()}), member, true},
		{"{} in [member]", FromSlice([]Value{member}), emptyMap(), false},

		{"member in [member]", FromSlice([]Value{member}), member, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.haystack.TryContains(tc.needle)
			if err != nil {
				t.Fatalf("TryContains: %v", err)
			}
			if got != tc.want {
				t.Errorf("TryContains = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestOpaqueMapOrderingFaults pins the `unreachable!()` path.
//
// Ordering two `ObjectRepr::Map` operands needs a pair iterator from BOTH sides,
// and the engine has no arm for a missing one. Reproducing the fault is the only
// outcome that is not a stronger answer than the engine's: an ordering result, a
// decline, or a plain false would each let a template render where BAML aborts.
func TestOpaqueMapOrderingFaults(t *testing.T) {
	member := FromObject(&opaqueCmpMap{canonical: "RED", display: "rouge"})
	namespace := FromObject(&opaqueMap{display: "Color"})

	cases := []struct {
		name  string
		left  Value
		right Value
		want  string
	}{
		{"member < namespace", member, namespace, "neither enumerates its entries"},
		{"namespace < member", namespace, member, "neither enumerates its entries"},
		{"member < {}", member, emptyMap(), "the left one does not enumerate its entries"},
		{"{} < member", emptyMap(), member, "the right one does not enumerate its entries"},
		{"{'a': 'b'} < member", mapWith("a", "b"), member, "the right one does not enumerate its entries"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatal("ordering did not fault; the engine reaches unreachable!() here")
				}
				fault, ok := r.(UnorderableMaps)
				if !ok {
					t.Fatalf("panicked with %T (%v), want UnorderableMaps", r, r)
				}
				if !strings.Contains(fault.Error(), tc.want) {
					t.Errorf("Error() = %q, want it to contain %q", fault.Error(), tc.want)
				}
				// The fault carries the operands, so a recovering host can
				// report what it was asked to order.
				if !fault.Left.Equal(tc.left) && fault.Left.String() != tc.left.String() {
					t.Errorf("Left = %v, want the left operand", fault.Left.String())
				}
			}()
			cmp, ok := tc.left.Compare(tc.right)
			t.Fatalf("Compare returned (%d, %v) instead of faulting", cmp, ok)
		})
	}
}

// TestOpaqueMapOrderingHookStillWins is the other half of the ordering
// contract: the fault is a LAST resort, reached only once the generic
// comparison hook has declined from both operand positions.
func TestOpaqueMapOrderingHookStillWins(t *testing.T) {
	red := FromObject(&opaqueCmpMap{canonical: "RED", display: "rouge"})
	blue := FromObject(&opaqueCmpMap{canonical: "BLUE", display: "bleu"})

	// Two members: the hook answers, so no fault.
	if cmp, ok := blue.Compare(red); !ok || cmp >= 0 {
		t.Errorf("Compare(BLUE, RED) = (%d, %v), want a negative answer from the hook", cmp, ok)
	}
	// And from the right operand position, negated.
	if cmp, ok := FromString("BLUE").Compare(red); !ok || cmp >= 0 {
		t.Errorf(`Compare("BLUE", RED) = (%d, %v), want a negative answer from the right operand's hook`, cmp, ok)
	}
}

// TestEnumerableMapsStillOrderStructurally guards the fault against
// over-reaching: two mappings that both enumerate are ordered as before.
func TestEnumerableMapsStillOrderStructurally(t *testing.T) {
	cases := []struct {
		name  string
		left  Value
		right Value
		want  int
	}{
		{"{} vs {}", emptyMap(), emptyMap(), 0},
		{"{} vs {'a': 'b'}", emptyMap(), mapWith("a", "b"), -1},
		{"{'a': 'b'} vs {'a': 'c'}", mapWith("a", "b"), mapWith("a", "c"), -1},
		{"{'a': 'b'} vs {'b': 'b'}", mapWith("a", "b"), mapWith("b", "b"), -1},
		{"host map vs {}", FromObject(&enumerableMap{m: NewOrderedMap(0)}), emptyMap(), 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmp, ok := tc.left.Compare(tc.right)
			if !ok {
				t.Fatal("Compare declined for two enumerable mappings")
			}
			if cmp != tc.want {
				t.Errorf("Compare = %d, want %d", cmp, tc.want)
			}
		})
	}
}
