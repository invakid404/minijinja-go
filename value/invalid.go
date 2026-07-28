package value

import "fmt"

// Invalid wraps an error as a value.
//
// The engine has a first-class representation for "this operation failed but
// the failure has to travel as a value" (`ValueRepr::Invalid`,
// value/mod.rs:428). Lazy iteration is where it becomes observable: `|chain`
// builds an iterable over its operands and, when one of them cannot be
// iterated, yields the error in that operand's place rather than dropping it
// (filters.rs:1574-1581). So
//
//	{{ 42|chain([1])|list }}
//
// renders `[<invalid value: invalid operation: number is not iterable>, 1]` —
// the failed operand is visible, and taking that element out of the list is an
// error rather than an empty slot.
//
// Where the error surfaces is the whole point, and it is not "when it is
// printed". The engine calls `validate()` at four places and nowhere else — a
// variable lookup, the result of an attribute lookup, the result of an item
// lookup, and each item an iteration yields (vm/mod.rs:287-295, 322-357,
// 479-484) — plus the `int` and `float` filters, which do it themselves. So
//
//	{{ 42|chain([1])|first }}          prints the marker
//	{{ (42|chain([1])|list)[0] == 1 }} fails
//	{% for x in 42|chain([1]) %}       fails
//	{{ (42|chain([1])|first).foo }}    is undefined, not an error
//
// The value itself is inert: it prints its marker wherever a container prints
// its elements, serializes as null, is not true, and answers InvalidError so
// the evaluator can reveal the error at those boundaries.
func Invalid(err error) Value {
	return FromObject(&invalidObject{err: err})
}

// InvalidError reports whether a value is an invalid value, and returns the
// error it carries. It is the fork's `validate()` (value/mod.rs:780-790): the
// evaluator calls it at the engine's validation boundaries, and nowhere else.
func InvalidError(v Value) (error, bool) {
	obj, ok := v.AsObject()
	if !ok {
		return nil, false
	}
	invalid, ok := obj.(*invalidObject)
	if !ok {
		return nil, false
	}
	return invalid.err, true
}

type invalidObject struct{ err error }

var (
	_ Object           = (*invalidObject)(nil)
	_ ObjectWithTruth  = (*invalidObject)(nil)
	_ ObjectWithString = (*invalidObject)(nil)
	_ fmt.Stringer     = (*invalidObject)(nil)
)

func (o *invalidObject) GetAttr(string) Value { return Undefined() }

// String is the marker the engine prints for an invalid value, in both its
// display and its debug form (value/mod.rs:446, 711). Value.Repr falls through
// to it for objects, which is what makes a list containing one render the way
// the engine renders it.
func (o *invalidObject) String() string { return fmt.Sprintf("<invalid value: %v>", o.err) }

func (o *invalidObject) ObjectString() string { return o.String() }

func (o *invalidObject) ObjectIsTrue() bool { return false }
