package minijinja

import (
	"strings"
	"testing"

	"github.com/invakid404/minijinja-go/v2/value"
)

// A host object's rendering must reach every consumer, not just top-level
// output.
//
// In the engine, `Object::render` is what an object value's Display AND its
// Debug both call (value/mod.rs:463, 717), so every filter that stringifies,
// every container that prints its elements and every coercion inherits it. The
// fork's counterpart is [value.ObjectWithString], dispatched by
// `value.Value.String` and `value.Value.Repr`.
//
// This is the composed half of that proof: `value/objectstring_fork_test.go`
// pins the two primitives, and these rows pin the consumers a template actually
// reaches through them. Every one of them was a Go pointer address before the
// dispatch existed.

// renderOnlyObject is a generic host object that implements the fork's render
// hook and NOTHING else — in particular it is not a [fmt.Stringer], so any path
// that still falls through to Go's `%v` renders a pointer and fails loudly.
type renderOnlyObject struct{ text string }

var (
	_ value.Object           = (*renderOnlyObject)(nil)
	_ value.ObjectWithString = (*renderOnlyObject)(nil)
)

func (o *renderOnlyObject) GetAttr(string) value.Value { return value.Undefined() }
func (o *renderOnlyObject) ObjectString() string       { return o.text }

// mapReprObject is the same thing with a map representation, which is the shape
// a host gives an opaque namespace-like global. Its rendering must come from the
// hook too, not from its (absent) pairs.
type mapReprObject struct{ text string }

var (
	_ value.Object           = (*mapReprObject)(nil)
	_ value.ObjectWithRepr   = (*mapReprObject)(nil)
	_ value.ObjectWithString = (*mapReprObject)(nil)
)

func (o *mapReprObject) GetAttr(string) value.Value   { return value.Undefined() }
func (o *mapReprObject) ObjectRepr() value.ObjectRepr { return value.ObjectReprMap }
func (o *mapReprObject) ObjectString() string         { return o.text }

func TestObjectStringReachesEveryConsumer(t *testing.T) {
	ctx := map[string]any{
		"red":  value.FromObject(&renderOnlyObject{text: "rouge"}),
		"blue": value.FromObject(&renderOnlyObject{text: "bleu"}),
		"ns":   value.FromObject(&mapReprObject{text: "Color"}),
	}

	for _, tc := range []struct{ name, source, want string }{
		// The control that was already green, and cannot detect the defect.
		{"top-level output", `{{ red }}`, "rouge"},

		// The native container paths, which recurse through Repr.
		{"native sequence", `{{ [red] }}`, "[rouge]"},
		{"two items", `{{ [red, blue] }}`, "[rouge, bleu]"},
		{"nested sequence", `{{ [[red]] }}`, "[[rouge]]"},
		{"as a mapping value", `{{ {"key1": red} }}`, `{"key1": rouge}`},
		{"through a sized iterable", `{{ [red, blue]|slice(1)|list }}`, "[[rouge, bleu]]"},

		// The coercion paths: `join` and `stringArg`, `Args.CoerceStr`, and the
		// explicit `|string` control.
		{"join", `{{ [red, blue]|join(",") }}`, "rouge,bleu"},
		{"upper", `{{ red|upper }}`, "ROUGE"},
		{"lower", `{{ red|upper|lower }}`, "rouge"},
		{"string filter", `{{ red|string }}`, "rouge"},
		{"length of the coerced string", `{{ red|string|length }}`, "5"},
		{"replace over the coerced string", `{{ red|replace("rou", "R") }}`, "Rge"},

		// The `~` operator, which is Value.Concat over two String() results.
		{"concat right", `{{ red ~ "!" }}`, "rouge!"},
		{"concat left", `{{ ">" ~ red }}`, ">rouge"},
		{"concat two objects", `{{ red ~ blue }}`, "rougebleu"},

		// A map-repr object renders from the hook rather than from its pairs.
		{"map-repr object", `{{ ns }}`, "Color"},
		{"map-repr object in a sequence", `{{ [ns] }}`, "[Color]"},
		{"map-repr object joined", `{{ [ns]|join(",") }}`, "Color"},

		// And through statement output, not only expression output.
		{"in a loop body", `{% for x in [red, blue] %}{{ x }};{% endfor %}`, "rouge;bleu;"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := renderExpr(t, tc.source, ctx)
			if got != tc.want {
				t.Errorf("%s = %q, want %q", tc.source, got, tc.want)
			}
			// The exact failure this closes is Go's `%v`, whose marks are the
			// struct literal `&{...}` and, for any field that is itself a
			// pointer, an address. Both can hide inside a longer rendering —
			// `{{ red|upper }}` was `&{ROUGE}` — so they are checked apart
			// from the equality above.
			if strings.Contains(got, "&{") || strings.Contains(got, "0x") {
				t.Errorf("%s leaked Go's own formatting: %q", tc.source, got)
			}
		})
	}
}
