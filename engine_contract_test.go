package minijinja

import (
	"strings"
	"testing"
)

// TestLoopCycleWithoutArgumentsFaults pins the one place this engine faults on
// template input instead of returning an error.
//
// BAML's engine computes `idx % args.len()` before it inspects the argument
// list (boundaryml/minijinja@8cfc770 vm/loop_object.rs call_method), so
// `{{ loop.cycle() }}` panics there with a remainder-by-zero. This engine
// reproduces that outcome rather than rendering a successful empty string the
// engine it mirrors cannot produce: the same expression, the same fault.
//
// Differential row: tmpl/loop-cycle-no-args, which compares panic against panic.
// Delta: PATCHES.md #9.
//
// The contract this test states, for callers: rendering is panic-free for every
// other input in the corpus, and a caller that renders untrusted templates
// should recover — the same precaution it would take around a Rust engine that
// can abort.
func TestLoopCycleWithoutArgumentsFaults(t *testing.T) {
	env := NewEnvironment()
	tmpl, err := env.TemplateFromNamedString("cycle.txt", `{% for x in [1] %}{{ loop.cycle() }}{% endfor %}`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	func() {
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("loop.cycle() with no arguments did not fault; the engine it mirrors panics here")
			}
			// Go words the same arithmetic fault differently from Rust; what is
			// pinned is that it is that fault, and that it is recoverable.
			if msg := toString(r); !strings.Contains(msg, "divide by zero") {
				t.Fatalf("faulted with %q, expected the remainder-by-zero fault", msg)
			}
		}()
		_, _ = tmpl.Render(nil)
	}()
}

// TestLoopCycleWithArgumentsDoesNotFault is the other half: the guard is absent
// on purpose, not by oversight, and every ordinary use still works.
func TestLoopCycleWithArgumentsDoesNotFault(t *testing.T) {
	env := NewEnvironment()
	tmpl, err := env.TemplateFromNamedString("cycle.txt",
		`{% for x in [1, 2, 3, 4] %}{{ loop.cycle('a', 'b') }}{% endfor %}`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	out, err := tmpl.Render(nil)
	if err != nil {
		t.Fatalf("render error: %v", err)
	}
	if out != "abab" {
		t.Errorf("expected %q, got %q", "abab", out)
	}
}

func toString(v any) string {
	if err, ok := v.(error); ok {
		return err.Error()
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
