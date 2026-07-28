package value

import (
	"strings"
	"testing"
)

// canonicalObject is a host object with a canonical comparison identity and a
// different display, which is the shape the generic value_cmp hook exists for:
// an enum-like value that compares by its variant name while rendering an alias.
type canonicalObject struct {
	canonical string
	display   string
}

func (o *canonicalObject) GetAttr(string) Value { return Undefined() }
func (o *canonicalObject) String() string       { return o.display }

func (o *canonicalObject) ValueCmp(other Value) (int, bool) {
	if s, ok := other.AsString(); ok {
		return strings.Compare(o.canonical, s), true
	}
	if obj, ok := other.AsObject(); ok {
		if oo, ok := obj.(*canonicalObject); ok {
			return strings.Compare(o.canonical, oo.canonical), true
		}
	}
	return 0, false
}

// plainCmpObject implements only the narrow object-to-object hook, to prove the
// generic dispatch still reaches it.
type plainCmpObject struct{ n int }

func (o *plainCmpObject) GetAttr(string) Value { return Undefined() }
func (o *plainCmpObject) String() string       { return "plain" }
func (o *plainCmpObject) ObjectCmp(other Object) (int, bool) {
	oo, ok := other.(*plainCmpObject)
	if !ok {
		return 0, false
	}
	return o.n - oo.n, true
}

// silentObject answers nothing, so comparison must fall through to the ordinary
// rules rather than treating "no opinion" as "not equal".
type silentObject struct{ label string }

func (o *silentObject) GetAttr(string) Value { return Undefined() }
func (o *silentObject) String() string       { return o.label }

func TestValueCmpHookEquality(t *testing.T) {
	red := FromObject(&canonicalObject{canonical: "RED", display: "Red"})
	rouge := FromObject(&canonicalObject{canonical: "RED", display: "Rouge"})
	blue := FromObject(&canonicalObject{canonical: "BLUE", display: "Blue"})

	cases := []struct {
		name string
		a, b Value
		want bool
	}{
		{"object vs canonical string", red, FromString("RED"), true},
		{"canonical string vs object", FromString("RED"), red, true},
		{"object vs display string", red, FromString("Red"), false},
		{"object vs other canonical", red, FromString("BLUE"), false},
		{"same canonical, different display", red, rouge, true},
		{"different canonical", red, blue, false},
		{"object vs number", red, FromInt(1), false},
		{"object vs none", red, None(), false},
		{"object vs undefined", red, Undefined(), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.a.Equal(c.b); got != c.want {
				t.Errorf("a == b = %v, want %v", got, c.want)
			}
			// Equality is symmetric: the hook is tried from both operand sides.
			if got := c.b.Equal(c.a); got != c.want {
				t.Errorf("b == a = %v, want %v (asymmetric equality)", got, c.want)
			}
		})
	}
}

func TestValueCmpHookOrdering(t *testing.T) {
	red := FromObject(&canonicalObject{canonical: "RED", display: "Red"})
	blue := FromObject(&canonicalObject{canonical: "BLUE", display: "Blue"})

	cases := []struct {
		name string
		a, b Value
		want int
	}{
		{"object vs greater string", red, FromString("ZZ"), -1},
		{"object vs lesser string", red, FromString("BLUE"), 1},
		{"object vs equal string", red, FromString("RED"), 0},
		// From the right-hand side the result must be negated, not reused.
		{"string vs greater object", FromString("BLUE"), red, -1},
		{"greater string vs object", FromString("ZZ"), red, 1},
		{"object vs object", blue, red, -1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := c.a.Compare(c.b)
			if !ok {
				t.Fatalf("Compare reported incomparable")
			}
			if sign(got) != c.want {
				t.Errorf("Compare = %d, want sign %d", got, c.want)
			}
		})
	}
}

// TestValueCmpBeforeKindOrdering pins the ordering rule that the hook runs
// before kind ordering: without that, a Plain object would always sort after a
// string simply because of its kind.
func TestValueCmpBeforeKindOrdering(t *testing.T) {
	red := FromObject(&canonicalObject{canonical: "RED", display: "Red"})
	if got, _ := red.Compare(FromString("ZZ")); sign(got) != -1 {
		t.Errorf("object vs string = %d, want less; kind ordering was applied first", got)
	}
}

// TestNarrowObjectCmpStillDispatches proves the older, object-only hook is not
// bypassed by the new one.
func TestNarrowObjectCmpStillDispatches(t *testing.T) {
	a := FromObject(&plainCmpObject{n: 1})
	b := FromObject(&plainCmpObject{n: 2})
	if !a.Equal(FromObject(&plainCmpObject{n: 1})) {
		t.Error("equal objects compared unequal")
	}
	if got, ok := a.Compare(b); !ok || sign(got) != -1 {
		t.Errorf("Compare = %d, ok = %v, want less", got, ok)
	}
}

// TestDecliningHookFallsThrough pins that "no opinion" is not "not equal": an
// object that answers nothing still gets the ordinary plain-object comparison,
// which is by rendering.
func TestDecliningHookFallsThrough(t *testing.T) {
	a := FromObject(&silentObject{label: "same"})
	b := FromObject(&silentObject{label: "same"})
	c := FromObject(&silentObject{label: "other"})
	if !a.Equal(b) {
		t.Error("two plain objects with the same rendering compared unequal")
	}
	if a.Equal(c) {
		t.Error("two plain objects with different renderings compared equal")
	}
	if got, ok := a.Compare(c); !ok || sign(got) != 1 {
		t.Errorf("Compare = %d, ok = %v, want greater (\"same\" > \"other\")", got, ok)
	}
}

// TestValueCmpHookReachedThroughContainers pins that the hook is reached by
// containment and by nested sequence equality, not only by a top-level ==.
func TestValueCmpHookReachedThroughContainers(t *testing.T) {
	red := FromObject(&canonicalObject{canonical: "RED", display: "Red"})

	if !FromSlice([]Value{red}).Contains(FromString("RED")) {
		t.Error("'RED' in [object] was false")
	}
	if !FromSlice([]Value{FromString("RED")}).Contains(red) {
		t.Error("object in ['RED'] was false")
	}
	if !FromSlice([]Value{red}).Equal(FromSlice([]Value{FromString("RED")})) {
		t.Error("[object] == ['RED'] was false")
	}
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	default:
		return 0
	}
}
