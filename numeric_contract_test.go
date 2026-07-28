package minijinja

import (
	"strings"
	"testing"
)

// The numeric slice needed one engine error form the port did not have: the
// detail-less `Error::from(ErrorKind)`, which renders as just the kind. The
// obvious implementation — treat an empty Message as "no detail" — is wrong,
// because an empty detail is still a detail and errors built from dynamic input
// can have one. These tests pin both halves of that distinction, so the numeric
// form cannot be reintroduced by inference and the public form cannot drift.

// TestDetailLessErrorRendersAsJustTheKind pins the numeric form.
//
// `ops::neg` on a non-number is `Err(Error::from(ErrorKind::InvalidOperation))`
// with no detail, and the corpus row num/neg-bool compares the rendered text
// against the engine's.
func TestDetailLessErrorRendersAsJustTheKind(t *testing.T) {
	env := NewEnvironment()
	tmpl, err := env.TemplateFromNamedString("t.txt", "{{ -true }}")
	if err != nil {
		t.Fatal(err)
	}
	_, err = tmpl.Render(nil)
	if err == nil {
		t.Fatal("negating a bool must fail")
	}
	got := err.Error()
	if !strings.HasPrefix(got, "invalid operation") {
		t.Fatalf("got %q, want it to start with %q", got, "invalid operation")
	}
	if strings.HasPrefix(got, "invalid operation:") {
		t.Errorf("got %q, want no detail separator: the engine renders this as just the kind", got)
	}
}

// TestEmptyMessageIsStillADetail pins the public form, which the numeric slice
// must NOT have changed.
//
// `NewError(kind, "")` is an error whose detail is empty, not an error with no
// detail. It renders with its separator exactly as it did before the numeric
// slice, and so does every reachable engine path that builds an error from a
// dynamic string — `GetTemplate("")` being the one the review found.
func TestEmptyMessageIsStillADetail(t *testing.T) {
	if got, want := NewError(ErrInvalidOperation, "").Error(), "invalid operation: "; got != want {
		t.Errorf("NewError(kind, \"\") = %q, want %q", got, want)
	}
	if got, want := NewError(ErrTemplateNotFound, "").Error(), "template not found: "; got != want {
		t.Errorf("NewError(ErrTemplateNotFound, \"\") = %q, want %q", got, want)
	}
	// The reachable path: the template name is passed straight through as the
	// detail, so an empty name yields an empty detail.
	//
	// This asserts the rendering is UNCHANGED by the numeric slice. It is not a
	// claim of parity with the engine, which renders `template "" does not
	// exist` here — a pre-existing difference in the template slice's error
	// surface, and not this slice's to change.
	env := NewEnvironment()
	if _, err := env.GetTemplate(""); err == nil {
		t.Error("GetTemplate(\"\") must fail")
	} else if got, want := err.Error(), "template not found: "; got != want {
		t.Errorf("GetTemplate(\"\") = %q, want %q", got, want)
	}

	// And the detail-less constructor is the only way to get the other form.
	if got, want := NewErrorWithoutDetail(ErrInvalidOperation).Error(), "invalid operation"; got != want {
		t.Errorf("NewErrorWithoutDetail(kind) = %q, want %q", got, want)
	}
}
