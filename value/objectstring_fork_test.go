package value

import "testing"

// [ObjectWithString] is the fork's `Object::render`, and it has to be dispatched
// by the two primitives everything else composes on.
//
// In the engine an object value's Display and its Debug are the SAME call
// (value/mod.rs:463 and 717 both reach `DynObject::render`). Every renderer,
// container and coercion in the engine is written on top of those two, so an
// object that overrides `render` — as BAML's enum, class and list objects do —
// is rendered correctly everywhere for free.
//
// The fork's counterparts are [Value.String] and [Value.Repr]. Until they
// dispatched [ObjectWithString], the interface was documented but dead: display
// fell through to Go's `%v`, so an object that implemented it and NOT
// [fmt.Stringer] printed a Go pointer wherever it appeared. Patching the
// consumers one at a time cannot fix that, because native sequences, iterators
// and `formatMap` all recurse back into `Repr`.
//
// The object below is deliberately minimal, and deliberately NOT a
// [fmt.Stringer]: `%v` on it prints a pointer, so any path that still reaches
// the Go fallback shows up as an address in the assertion rather than silently
// passing.

// renderObject implements ObjectWithString and nothing else.
type renderObject struct{ text string }

var (
	_ Object           = (*renderObject)(nil)
	_ ObjectWithString = (*renderObject)(nil)
)

func (r *renderObject) GetAttr(string) Value { return Undefined() }
func (r *renderObject) ObjectString() string { return r.text }

// TestObjectStringDispatchedByStringAndRepr is the root of the fix: both
// primitives consult the object, and neither reaches `%v`.
func TestObjectStringDispatchedByStringAndRepr(t *testing.T) {
	v := FromObject(&renderObject{text: "rouge"})

	if got := v.String(); got != "rouge" {
		t.Errorf("String() = %q, want %q", got, "rouge")
	}
	// Debug is the same call in the engine, so it is the same answer here —
	// NOT a quoted or escaped form of it.
	if got := v.Repr(); got != "rouge" {
		t.Errorf("Repr() = %q, want %q", got, "rouge")
	}
}

// TestObjectStringThroughNativeContainers is why the fix belongs in the
// primitives. A native sequence, an iterator and a mapping each render their
// contained values through Repr, so they inherit the dispatch rather than
// needing one of their own.
func TestObjectStringThroughNativeContainers(t *testing.T) {
	red := FromObject(&renderObject{text: "rouge"})
	blue := FromObject(&renderObject{text: "bleu"})

	cases := []struct {
		name string
		val  Value
		want string
		// nested is what the container renders as when it is itself an item of
		// a sequence. It is "[" + want + "]" for everything whose debug and
		// display forms agree; an iterator is the exception, because the engine
		// refuses to iterate an unsized iterable while printing it.
		nested string
	}{
		{name: "native sequence", val: FromSlice([]Value{red}), want: "[rouge]"},
		{name: "two items", val: FromSlice([]Value{red, blue}), want: "[rouge, bleu]"},
		{name: "nested sequence", val: FromSlice([]Value{FromSlice([]Value{red})}), want: "[[rouge]]"},
		{
			name:   "iterator",
			val:    FromIterator(NewIterator("test", []Value{red, blue})),
			want:   "[rouge, bleu]",
			nested: "[<iterator>]",
		},
		{name: "mapping value", val: func() Value {
			m := NewOrderedMap(1)
			m.Set("key1", red)
			return FromOrderedMap(m)
		}(), want: `{"key1": rouge}`},
		{name: "sequence inside a mapping", val: func() Value {
			m := NewOrderedMap(1)
			m.Set("key1", FromSlice([]Value{red}))
			return FromOrderedMap(m)
		}(), want: `{"key1": [rouge]}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.val.String(); got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
			nested := tc.nested
			if nested == "" {
				nested = "[" + tc.want + "]"
			}
			if got := FromSlice([]Value{tc.val}).String(); got != nested {
				t.Errorf("nested String() = %q, want %q", got, nested)
			}
		})
	}
}

// TestObjectStringThroughCoercion covers the consumers that take a value AS a
// string. `Value.String` is the whole of that contract in this package —
// `filters.stringArg`, `Args.CoerceStr`/`OptCoerceStr` and the environment's
// output formatter all call it — and `~` is the one such consumer that lives
// here.
func TestObjectStringThroughCoercion(t *testing.T) {
	red := FromObject(&renderObject{text: "rouge"})

	if got := red.Concat(FromString("!")).String(); got != "rouge!" {
		t.Errorf("Concat = %q, want %q", got, "rouge!")
	}
	if got := FromString(">").Concat(red).String(); got != ">rouge" {
		t.Errorf("Concat = %q, want %q", got, ">rouge")
	}

	// An object is not a string, so `AsString` still declines: coercion is the
	// consumer's decision, not a change to what the value IS.
	if s, ok := red.AsString(); ok {
		t.Errorf("AsString() = (%q, true), want ok == false for an object", s)
	}
}

// TestObjectStringPrefersTheHookOverStringer pins the priority between the two
// spellings a host may use. The interface wins; `%v`, and with it
// [fmt.Stringer], stays as the fallback so existing objects keep working.
func TestObjectStringPrefersTheHookOverStringer(t *testing.T) {
	both := FromObject(&bothSpellings{})
	if got := both.String(); got != "from ObjectString" {
		t.Errorf("String() = %q, want the ObjectWithString answer", got)
	}
	if got := both.Repr(); got != "from ObjectString" {
		t.Errorf("Repr() = %q, want the ObjectWithString answer", got)
	}

	only := FromObject(&stringerOnly{})
	if got := only.String(); got != "from Stringer" {
		t.Errorf("String() = %q, want the fmt.Stringer answer", got)
	}
	if got := FromSlice([]Value{only}).String(); got != "[from Stringer]" {
		t.Errorf("nested String() = %q, want %q", got, "[from Stringer]")
	}
}

type bothSpellings struct{}

func (b *bothSpellings) GetAttr(string) Value { return Undefined() }
func (b *bothSpellings) ObjectString() string { return "from ObjectString" }
func (b *bothSpellings) String() string       { return "from Stringer" }

type stringerOnly struct{}

func (s *stringerOnly) GetAttr(string) Value { return Undefined() }
func (s *stringerOnly) String() string       { return "from Stringer" }
