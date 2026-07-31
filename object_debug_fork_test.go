package minijinja

import (
	"strings"
	"testing"

	"github.com/invakid404/minijinja-go/v2/filters"
	"github.com/invakid404/minijinja-go/v2/value"
)

// The ALTERNATE debug renderers — `pprint` and `debug()` — must select an
// object's render the same way the engine does. In the engine `{value:#?}` of an
// object value calls `DynObject::render` (value/mod.rs:462), and a CUSTOM render
// wins over the default `debug_map`/`debug_list` (value/object.rs:331-352). So a
// host object with its own string form prints THAT, re-indented for its nesting
// depth, rather than a map rebuilt from its (canonical) keys.
//
// The bug these close: both renderers branched on the value's KIND and expanded
// an enumerable map from `MapKeys` before consulting the object render, so a
// class whose render is alias-aware (`{"key1": ...}`) printed its canonical
// field name (`{"prop1": ...}`) under `pprint`/`debug` while `{{ c }}` and every
// other path already showed the alias.

// classObject is the shape of a BAML class value: an ENUMERABLE mapping (keys
// through MapObject, values through GetAttr) whose render is alias-aware and
// pretty-printed. AsMap declines it, so map methods and dictsort reach it
// through the generic MapKeys/GetItem path.
type classObject struct {
	m       *value.OrderedMap
	display string
}

var (
	_ value.Object           = (*classObject)(nil)
	_ value.ObjectWithRepr   = (*classObject)(nil)
	_ value.ObjectWithString = (*classObject)(nil)
	_ value.MapObject        = (*classObject)(nil)
)

func (o *classObject) ObjectRepr() value.ObjectRepr { return value.ObjectReprMap }
func (o *classObject) Keys() []string               { return o.m.Keys() }
func (o *classObject) ObjectString() string         { return o.display }
func (o *classObject) GetAttr(name string) value.Value {
	v, ok := o.m.Get(name)
	if !ok {
		return value.Undefined()
	}
	return v
}

func classCtx() map[string]any {
	m := value.NewOrderedMap(1)
	m.Set("prop1", value.FromString("value"))
	return map[string]any{
		// The class renders `{"key1": "value"}` pretty-printed — its ObjectString
		// is exactly the object render BAML's class produces (alias-aware).
		"c":      value.FromObject(&classObject{m: m, display: "{\n    \"key1\": \"value\",\n}"}),
		"member": value.FromObject(&mapReprObject{text: "rouge"}),
		"ns":     value.FromObject(&mapReprObject{text: "{}"}),
	}
}

func renderErr(t *testing.T, source string, ctx map[string]any) (string, error) {
	t.Helper()
	env := NewEnvironment()
	tmpl, err := env.TemplateFromNamedString("t.txt", source)
	if err != nil {
		t.Fatalf("parse %q: %v", source, err)
	}
	return tmpl.Render(ctx)
}

// TestPprintDebugHonorObjectRender is finding #2: the class's alias-aware render
// wins over generic map expansion in both alternate-debug renderers, at the top
// level and nested, where Rust's DebugList/DebugMap re-indent the entry.
func TestPprintDebugHonorObjectRender(t *testing.T) {
	direct := "{\n    \"key1\": \"value\",\n}"
	inList := "[\n    {\n        \"key1\": \"value\",\n    },\n]"
	inMap := "{\n    \"c\": {\n        \"key1\": \"value\",\n    },\n}"

	cases := []struct{ name, source, want string }{
		// Direct: pprint and debug are the SAME `{:#?}` call, so the same bytes.
		{"pprint", `{{ c|pprint }}`, direct},
		{"debug", `{{ debug(c) }}`, direct},

		// Nested: the object render is re-indented by the surrounding depth.
		{"pprint in a list", `{{ [c]|pprint }}`, inList},
		{"debug in a list", `{{ debug([c]) }}`, inList},
		{"pprint in a map", `{{ {"c": c}|pprint }}`, inMap},

		// The enum-like objects render bare, at every depth.
		{"enum member pprint", `{{ member|pprint }}`, "rouge"},
		{"enum member debug", `{{ debug(member) }}`, "rouge"},
		{"namespace pprint", `{{ ns|pprint }}`, "{}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := renderExpr(t, tc.source, classCtx())
			if got != tc.want {
				t.Errorf("%s = %q, want %q", tc.source, got, tc.want)
			}
			// Never the canonical field name, and never a Go pointer.
			if strings.Contains(got, "prop1") {
				t.Errorf("%s leaked the canonical key: %q", tc.source, got)
			}
			if strings.Contains(got, "&{") || strings.Contains(got, "0x") {
				t.Errorf("%s leaked Go's own formatting: %q", tc.source, got)
			}
		})
	}
}

// TestDictsortAndItemsOnHostMap is finding #3 through the two map filters: an
// enumerable class map (which AsMap declines) still sorts and items by its
// canonical keys, while a non-enumerable map faults the way the engine's
// `ok!(v.try_iter())` does rather than sorting an empty list.
func TestDictsortAndItemsOnHostMap(t *testing.T) {
	// Enumerable class map: pairs by canonical key.
	for _, tc := range []struct{ name, source, want string }{
		{"dictsort", `{{ c|dictsort }}`, `[["prop1", "value"]]`},
		{"items control", `{{ c|items|list }}`, `[["prop1", "value"]]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := renderExpr(t, tc.source, classCtx())
			if got != tc.want {
				t.Errorf("%s = %q, want %q", tc.source, got, tc.want)
			}
		})
	}

	// Non-enumerable map: dictsort faults like the engine.
	t.Run("dictsort faults on non-enumerable map", func(t *testing.T) {
		out, err := renderErr(t, `{{ member|dictsort }}`, classCtx())
		if err == nil {
			t.Fatalf("rendered %q, want a `map is not iterable` fault", out)
		}
		if !strings.Contains(err.Error(), "map is not iterable") {
			t.Errorf("error = %v, want it to contain %q", err, "map is not iterable")
		}
	})

	// A non-map is still the other invalid operation.
	t.Run("dictsort on a non-map", func(t *testing.T) {
		_, err := renderErr(t, `{{ 5|dictsort }}`, nil)
		if err == nil || !strings.Contains(err.Error(), "cannot convert value into pair list") {
			t.Errorf("error = %v, want `cannot convert value into pair list`", err)
		}
	})
}

// TestUrlencodeHostMap is the swept sibling of finding #1/#3: urlencode's map
// branch used AsMap, so a non-enumerable map fell through to string coercion and
// SUCCEEDED (an out-do — stock's `ok!(value.try_iter())` errors), and an
// enumerable host map that AsMap declined was string-coerced too. It is a
// withdrawn filter (not in the default environment), so this registers it
// explicitly to exercise the exported function's parity for any external caller.
func TestUrlencodeHostMap(t *testing.T) {
	newEnv := func() *Environment {
		env := NewEnvironment()
		env.AddFilter("urlencode", filters.FilterUrlencode)
		return env
	}

	// Enumerable host map that AsMap does not recognise: a real query string.
	t.Run("enumerable host map", func(t *testing.T) {
		tmpl, err := newEnv().TemplateFromNamedString("t.txt", `{{ c|urlencode }}`)
		if err != nil {
			t.Fatal(err)
		}
		out, err := tmpl.Render(classCtx())
		if err != nil {
			t.Fatalf("urlencode of a class map errored: %v", err)
		}
		if out != "prop1=value" {
			t.Errorf("urlencode(c) = %q, want %q", out, "prop1=value")
		}
	})

	// Non-enumerable map: faults, never a coerced string.
	t.Run("non-enumerable map faults", func(t *testing.T) {
		tmpl, err := newEnv().TemplateFromNamedString("t.txt", `{{ member|urlencode }}`)
		if err != nil {
			t.Fatal(err)
		}
		out, err := tmpl.Render(classCtx())
		if err == nil {
			t.Fatalf("urlencode of a non-enumerable map rendered %q, want a fault", out)
		}
		if !strings.Contains(err.Error(), "map is not iterable") {
			t.Errorf("error = %v, want `map is not iterable`", err)
		}
	})
}

// TestJSONAutoEscapeHostMap is the swept sibling of finding #3 on the JSON
// auto-escape path: valueToNative used AsMap, so an enumerable host map that is
// not a Go map serialized as an empty `{}` — silent data loss. It is reachable
// only under JSON auto-escape (a `.json` template), which BAML's text prompts do
// not use, but the value model should not drop a host map's entries.
func TestJSONAutoEscapeHostMap(t *testing.T) {
	render := func(t *testing.T, source string) string {
		t.Helper()
		tmpl, err := NewEnvironment().TemplateFromNamedString("data.json", source)
		if err != nil {
			t.Fatal(err)
		}
		out, err := tmpl.Render(classCtx())
		if err != nil {
			t.Fatalf("render %q: %v", source, err)
		}
		return out
	}

	// The enumerable class map serializes its entries, not an empty object.
	if got := render(t, `{{ c }}`); got != `{"prop1":"value"}` {
		t.Errorf(`JSON of c = %q, want %q`, got, `{"prop1":"value"}`)
	}
	// A non-enumerable map has no pairs, so it stays {} on both sides.
	if got := render(t, `{{ member }}`); got != `{}` {
		t.Errorf(`JSON of member = %q, want "{}"`, got)
	}
}
