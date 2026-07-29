package value

import (
	"errors"
	"testing"
)

// TestInvalidValue pins the contract the lazy chain relies on: an invalid
// value has its own kind, prints the engine's marker wherever a container
// prints its elements, answers InvalidError so the evaluator can reveal the
// error at a validation boundary, and is not true.
func TestInvalidValue(t *testing.T) {
	cause := errors.New("invalid operation: number is not iterable")
	val := Invalid(cause)

	const marker = "<invalid value: invalid operation: number is not iterable>"
	if got := val.String(); got != marker {
		t.Errorf("String() = %q, want %q", got, marker)
	}
	if got := val.Repr(); got != marker {
		t.Errorf("Repr() = %q, want %q", got, marker)
	}

	// Inside a sequence, which is where the engine actually shows it.
	list := FromSlice([]Value{val, FromInt(1)})
	if want := "[" + marker + ", 1]"; list.String() != want {
		t.Errorf("list rendering = %q, want %q", list.String(), want)
	}

	if val.Kind() != KindInvalid {
		t.Errorf("Kind() = %v, want %v", val.Kind(), KindInvalid)
	}

	got, ok := InvalidError(val)
	if !ok {
		t.Fatal("InvalidError did not recognise an invalid value")
	}
	if !errors.Is(got, cause) {
		t.Errorf("InvalidError returned %v, want %v", got, cause)
	}
	if val.IsTrue() {
		t.Error("an invalid value is true")
	}

	if _, ok := InvalidError(FromInt(1)); ok {
		t.Error("InvalidError recognised a number as an invalid value")
	}
	if _, ok := InvalidError(Undefined()); ok {
		t.Error("InvalidError recognised undefined as an invalid value")
	}
}
