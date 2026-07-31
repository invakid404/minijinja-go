package pycompat_test

import (
	"strings"
	"testing"

	minijinja "github.com/invakid404/minijinja-go/v2"
	"github.com/invakid404/minijinja-go/v2/pycompat"
	"github.com/invakid404/minijinja-go/v2/value"
)

// These pin the pycompat module against a host map that is a map by
// REPRESENTATION but does not enumerate its pairs (`Enumerator::NonEnumerable`),
// and against one that enumerates through a MapObject while rendering through a
// custom `ObjectString`. No BAML profile is involved — these are the generic
// shapes any host may install, exactly as `value/opaquemap_fork_test.go` uses
// them for the value-model seam.
//
// The bug they close: `str.join` treated a non-enumerable map as an empty
// iterable and rendered "" where the engine's `values.try_iter()?` raises
// `map is not iterable`, an out-do; and the map-method table declined a host
// map that AsMap did not recognise, an under-do where the engine answers.

// pyOpaqueMap is a non-enumerable mapping with its own render, the shape of a
// BAML enum member or namespace: ObjectReprMap, no MapObject/MapGetter.
type pyOpaqueMap struct{ display string }

var (
	_ value.Object           = (*pyOpaqueMap)(nil)
	_ value.ObjectWithRepr   = (*pyOpaqueMap)(nil)
	_ value.ObjectWithString = (*pyOpaqueMap)(nil)
)

func (o *pyOpaqueMap) GetAttr(string) value.Value   { return value.Undefined() }
func (o *pyOpaqueMap) ObjectRepr() value.ObjectRepr { return value.ObjectReprMap }
func (o *pyOpaqueMap) ObjectString() string         { return o.display }

// pyRenderMap is an ENUMERABLE mapping that is not a Go map: it exposes its keys
// through MapObject and its values through GetAttr, and renders through a custom
// ObjectString. This is the shape of a BAML class value — AsMap declines it
// (it is neither a Go map nor a MapGetter), so it exercises the generic
// MapKeys/GetItem path the fix installs.
type pyRenderMap struct {
	m       *value.OrderedMap
	display string
}

var (
	_ value.Object           = (*pyRenderMap)(nil)
	_ value.ObjectWithRepr   = (*pyRenderMap)(nil)
	_ value.ObjectWithString = (*pyRenderMap)(nil)
	_ value.MapObject        = (*pyRenderMap)(nil)
)

func (o *pyRenderMap) ObjectRepr() value.ObjectRepr { return value.ObjectReprMap }
func (o *pyRenderMap) Keys() []string               { return o.m.Keys() }
func (o *pyRenderMap) ObjectString() string         { return o.display }
func (o *pyRenderMap) GetAttr(name string) value.Value {
	v, ok := o.m.Get(name)
	if !ok {
		return value.Undefined()
	}
	return v
}

func renderWith(t *testing.T, source string, globals map[string]value.Value) (string, error) {
	t.Helper()
	env := minijinja.NewEnvironment()
	env.SetUnknownMethodCallback(pycompat.UnknownMethodCallback)
	for k, v := range globals {
		env.AddGlobal(k, v)
	}
	tmpl, err := env.TemplateFromNamedString("t.txt", source)
	if err != nil {
		t.Fatalf("parse %q: %v", source, err)
	}
	return tmpl.Render(nil)
}

func classGlobal() map[string]value.Value {
	m := value.NewOrderedMap(1)
	m.Set("prop1", value.FromString("value"))
	return map[string]value.Value{
		// A non-enumerable member and namespace, plus an enumerable class map.
		"member": value.FromObject(&pyOpaqueMap{display: "rouge"}),
		"ns":     value.FromObject(&pyOpaqueMap{display: "Color"}),
		"c":      value.FromObject(&pyRenderMap{m: m, display: "{\n    \"key1\": \"value\",\n}"}),
	}
}

// TestJoinFaultsOnNonEnumerableMap is finding #1: `str.join` over a
// non-enumerable map must fault exactly as the engine does, not render "".
func TestJoinFaultsOnNonEnumerableMap(t *testing.T) {
	for _, source := range []string{
		`{{ ",".join(member) }}`,
		`{{ ",".join(ns) }}`,
	} {
		t.Run(source, func(t *testing.T) {
			out, err := renderWith(t, source, classGlobal())
			if err == nil {
				t.Fatalf("rendered %q, want a `map is not iterable` fault", out)
			}
			if !strings.Contains(err.Error(), "map is not iterable") {
				t.Errorf("error = %v, want it to contain %q", err, "map is not iterable")
			}
		})
	}
}

// TestJoinStillEmptyOnEnumerableEmptyMap is the other side of the fix: a
// KNOWN-EMPTY enumerable map still joins to "", because its `Iter()` is a
// non-nil empty slice. Only the non-enumerable map faults.
func TestJoinStillEmptyOnEnumerableEmptyMap(t *testing.T) {
	got, err := render(t, `{{ ",".join({}) }}`)
	if err != nil {
		t.Fatalf("join over {} errored: %v", err)
	}
	if got != "" {
		t.Errorf("join over {} = %q, want \"\"", got)
	}
}

// TestJoinOverEnumerableMapJoinsKeys pins the Python semantics the fix keeps:
// joining a mapping joins its keys.
func TestJoinOverEnumerableMapJoinsKeys(t *testing.T) {
	got, err := renderWith(t, `{{ ",".join(c) }}`, classGlobal())
	if err != nil {
		t.Fatalf("join over an enumerable host map errored: %v", err)
	}
	if got != "prop1" {
		t.Errorf("join over c = %q, want %q (its keys)", got, "prop1")
	}
}

// TestMapMethodsOnNonEnumerableMap is the under-do half of finding #3: the
// engine answers keys/values/items/get on any Map-representation object, so a
// non-enumerable one yields empty views and get-fallback — it does NOT decline
// with `unknown method`.
func TestMapMethodsOnNonEnumerableMap(t *testing.T) {
	cases := []struct{ source, want string }{
		{`{{ member.keys()|list }}`, "[]"},
		{`{{ member.values()|list }}`, "[]"},
		{`{{ member.items()|list }}`, "[]"},
		{`{{ member.get("x", "fallback") }}`, "fallback"},
	}
	for _, tc := range cases {
		t.Run(tc.source, func(t *testing.T) {
			got, err := renderWith(t, tc.source, classGlobal())
			if err != nil {
				t.Fatalf("%s errored: %v", tc.source, err)
			}
			if got != tc.want {
				t.Errorf("%s = %q, want %q", tc.source, got, tc.want)
			}
		})
	}
}

// TestMapMethodsOnEnumerableHostMap is the reachable half of finding #3: a host
// class map that AsMap does not recognise still answers keys/values/items/get
// through the generic MapKeys/GetItem path, by its CANONICAL keys.
func TestMapMethodsOnEnumerableHostMap(t *testing.T) {
	cases := []struct{ source, want string }{
		{`{{ c.keys()|list }}`, `["prop1"]`},
		{`{{ c.values()|list }}`, `["value"]`},
		{`{{ c.items()|list }}`, `[["prop1", "value"]]`},
		{`{{ c.get("prop1", "fallback") }}`, "value"},
		{`{{ c.get("key1", "fallback") }}`, "fallback"},
	}
	for _, tc := range cases {
		t.Run(tc.source, func(t *testing.T) {
			got, err := renderWith(t, tc.source, classGlobal())
			if err != nil {
				t.Fatalf("%s errored: %v", tc.source, err)
			}
			if got != tc.want {
				t.Errorf("%s = %q, want %q", tc.source, got, tc.want)
			}
		})
	}
}
