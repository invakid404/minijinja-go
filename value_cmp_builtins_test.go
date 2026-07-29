package minijinja

import (
	"testing"

	"github.com/invakid404/minijinja-go/v2/value"
)

// identityObject is a generic host object whose DISPLAY is deliberately the
// same for every instance and whose IDENTITY is an internal id answered
// through the engine's comparison hook.
//
// It is the shape that separates "compared by the value model" from "compared
// by whatever it renders as": two instances are indistinguishable in output
// and distinct to `value_cmp`.
type identityObject struct {
	id int
}

func (o *identityObject) GetAttr(string) value.Value { return value.Undefined() }

func (o *identityObject) String() string { return "same" }

// ValueCmp is the generic counterpart of `Object::value_cmp`, the one delta
// BAML's engine carries. Ordering against another instance is by id;
// everything else is left to the value model.
func (o *identityObject) ValueCmp(other value.Value) (int, bool) {
	obj, ok := other.AsObject()
	if !ok {
		return 0, false
	}
	rhs, ok := obj.(*identityObject)
	if !ok {
		return 0, false
	}
	switch {
	case o.id < rhs.id:
		return -1, true
	case o.id > rhs.id:
		return 1, true
	}
	return 0, true
}

func (o *identityObject) ObjectCmp(other value.Object) (int, bool) {
	rhs, ok := other.(*identityObject)
	if !ok {
		return 0, false
	}
	return o.ValueCmp(value.FromObject(rhs))
}

func renderExpr(t *testing.T, source string, ctx map[string]any) string {
	t.Helper()
	env := NewEnvironment()
	tmpl, err := env.TemplateFromNamedString("t.txt", source)
	if err != nil {
		t.Fatalf("parse %q: %v", source, err)
	}
	out, err := tmpl.Render(ctx)
	if err != nil {
		t.Fatalf("render %q: %v", source, err)
	}
	return out
}

// TestUniqueDedupesThroughTheComparisonHook pins that `unique` remembers what
// it has seen in the value model's own total ORDER rather than by a rendered
// key.
//
// The engine's memo is a `BTreeSet<Value>` (filters.rs:1500-1531), so
// membership goes through `Ord for Value`, which consults the generic
// comparison hook before anything else. Only a value that IS a string is
// memoized case-folded. Keying every non-string by its display string instead
// collapsed two distinct host objects into one — the fork bypassing its own
// comparison hook, with no pycompat or BAML type involved.
func TestUniqueDedupesThroughTheComparisonHook(t *testing.T) {
	ctx := map[string]any{
		"a": value.FromObject(&identityObject{id: 1}),
		"b": value.FromObject(&identityObject{id: 2}),
	}

	// The premise: the two objects are indistinguishable in output.
	if got := renderExpr(t, `{{ a }}|{{ b }}`, ctx); got != "same|same" {
		t.Fatalf("the fixture does not render alike: %q", got)
	}

	for _, tc := range []struct{ name, source, want string }{
		{"distinct identities stay distinct", `{{ [a,b]|unique|length }}`, "2"},
		{"a repeat of one identity is one", `{{ [a,b,a]|unique|length }}`, "2"},
		{"the same identity twice is one", `{{ [a,a]|unique|length }}`, "1"},
		{"case_sensitive does not change an object", `{{ [a,b]|unique(case_sensitive=true)|length }}`, "2"},
		{"through an attribute path too", `{{ [dict(k=a),dict(k=b)]|unique(attribute='k')|length }}`, "2"},
		// The neighbours that prove the memo is the ORDER and not identity:
		// numbers that coerce equal are one entry, and a bool is its own kind.
		{"int and float coerce to one entry", `{{ [1,1.0]|unique|length }}`, "1"},
		{"a bool keeps its own kind", `{{ [1,true]|unique|length }}`, "2"},
		// And that an actual string is still memoized case-folded.
		{"strings still fold", `{{ ['CA','ca']|unique|length }}`, "1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := renderExpr(t, tc.source, ctx); got != tc.want {
				t.Errorf("%s = %q, want %q", tc.source, got, tc.want)
			}
		})
	}
}
