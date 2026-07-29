package pycompat_test

import (
	"errors"
	"testing"
	"time"

	minijinja "github.com/invakid404/minijinja-go/v2"
	"github.com/invakid404/minijinja-go/v2/pycompat"
)

func render(t *testing.T, source string) (string, error) {
	t.Helper()
	env := minijinja.NewEnvironment()
	env.SetUnknownMethodCallback(pycompat.UnknownMethodCallback)
	tmpl, err := env.TemplateFromNamedString("t.txt", source)
	if err != nil {
		t.Fatalf("parse %q: %v", source, err)
	}
	return tmpl.Render(nil)
}

// TestNotInstalledByDefault is the contract that makes this a module rather
// than an engine builtin: a stock environment must not answer Python methods.
func TestNotInstalledByDefault(t *testing.T) {
	env := minijinja.NewEnvironment()
	tmpl, err := env.TemplateFromNamedString("t.txt", `{{ "x".upper() }}`)
	if err != nil {
		t.Fatal(err)
	}
	out, err := tmpl.Render(nil)
	if err == nil {
		t.Fatalf("rendered %q, want an unknown-method error", out)
	}
	var mjErr *minijinja.Error
	if !errors.As(err, &mjErr) || mjErr.Kind != minijinja.ErrUnknownMethod {
		t.Fatalf("error %v, want kind %v", err, minijinja.ErrUnknownMethod)
	}
}

func TestMethods(t *testing.T) {
	cases := []struct{ source, want string }{
		{`{{ "hello".upper() }}`, "HELLO"},
		{`{{ "Straße".upper() }}`, "STRASSE"},
		{`{{ "ΑΣ".lower() }}`, "ας"},
		{`{{ "abc".islower() }}`, "true"},
		{`{{ "".islower() }}`, "true"},
		{`{{ "  hi  ".strip() }}`, "hi"},
		{`{{ "xxhixx".strip("x") }}`, "hi"},
		{`{{ "aaa".replace("a", "b", 2) }}`, "bba"},
		{`{{ "a!b".title() }}`, "A!B"},
		{`{{ "a b  c".split() }}`, `["a", "b", "c"]`},
		{`{{ "banana".count("an") }}`, "2"},
		{`{{ "é x".find("x") }}`, "3"},
		{`{{ "foobar".startswith(["x", "foo"]) }}`, "true"},
		{`{{ ", ".join([1, 2]) }}`, "1, 2"},
		{`{{ "{} {}".format("a", "b") }}`, "a b"},
		{`{{ "{:>4}|".format("ab") }}`, "  ab|"},
		{`{{ "{:,}".format(1234567) }}`, "1,234,567"},
		{`{{ dict(a=1).keys() }}`, `["a"]`},
		{`{{ dict(a=1).items() }}`, `[["a", 1]]`},
		{`{{ dict(a=1).get("zz", 5) }}`, "5"},
		{`{{ [1, 2, 1].count(1) }}`, "2"},
	}
	for _, tc := range cases {
		got, err := render(t, tc.source)
		if err != nil {
			t.Errorf("%s: %v", tc.source, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s = %q, want %q", tc.source, got, tc.want)
		}
	}
}

func TestErrors(t *testing.T) {
	cases := []struct {
		source string
		kind   minijinja.ErrorKind
	}{
		{`{{ "x".nope() }}`, minijinja.ErrUnknownMethod},
		{`{{ (42).upper() }}`, minijinja.ErrUnknownMethod},
		{`{{ "x".upper("y") }}`, minijinja.ErrTooManyArguments},
		{`{{ "x".upper(a=1) }}`, minijinja.ErrTooManyArguments},
		{`{{ "x".find() }}`, minijinja.ErrMissingArgument},
		{`{{ "x".find(1) }}`, minijinja.ErrInvalidOperation},
		{`{{ "x".startswith(1) }}`, minijinja.ErrInvalidOperation},
		{`{{ "{:d}".format("a") }}`, minijinja.ErrInvalidOperation},
	}
	for _, tc := range cases {
		out, err := render(t, tc.source)
		if err == nil {
			t.Errorf("%s rendered %q, want %v", tc.source, out, tc.kind)
			continue
		}
		var mjErr *minijinja.Error
		if !errors.As(err, &mjErr) {
			t.Errorf("%s: error %T, want *minijinja.Error", tc.source, err)
			continue
		}
		if mjErr.Kind != tc.kind {
			t.Errorf("%s: kind %v, want %v", tc.source, mjErr.Kind, tc.kind)
		}
	}
}

// TestCountEmptyNeedleTerminates pins the one deliberate departure from the
// reference module. `str.count` there walks the string with `find` and advances
// by the needle's length, which never advances for an empty needle: the engine
// loops forever. There is no corpus row for it — a row that does not terminate
// cannot be run — so the behaviour is pinned here and logged in PATCHES.md.
func TestCountEmptyNeedleTerminates(t *testing.T) {
	done := make(chan string, 1)
	go func() {
		out, err := render(t, `{{ "abc".count("") }}`)
		if err != nil {
			done <- "error: " + err.Error()
			return
		}
		done <- out
	}()
	select {
	case got := <-done:
		if got != "4" {
			t.Errorf(`"abc".count("") = %s, want 4 (Python's answer)`, got)
		}
	// Long enough that a slow machine is not mistaken for a hang, short enough
	// that a real hang fails this test rather than the whole run.
	case <-time.After(10 * time.Second):
		t.Fatal(`"abc".count("") did not terminate`)
	}
}
