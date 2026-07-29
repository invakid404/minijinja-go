package minijinja

import (
	"errors"
	"fmt"
	"testing"

	"github.com/invakid404/minijinja-go/v2/value"
)

// hostObject is a generic host object with one method of its own. Nothing
// about it is BAML- or pycompat-specific: it exists to pin the dispatch order
// the engine defines for `a.b()` (value/mod.rs:1611-1643), independently of
// whichever method module a host happens to install.
type hostObject struct {
	// declineAll makes every method call decline, so the object never answers
	// and the environment's callback is always the one that decides.
	declineAll bool
}

func (h *hostObject) GetAttr(name string) value.Value {
	if name == "attr" {
		return value.FromString("attribute")
	}
	return value.Undefined()
}

func (h *hostObject) CallMethod(_ value.State, name string, args []value.Value, _ *value.OrderedMap) (value.Value, error) {
	if h.declineAll {
		return value.Undefined(), value.ErrUnknownMethod
	}
	switch name {
	case "own":
		return value.FromString(fmt.Sprintf("own(%d)", len(args))), nil
	case "boom":
		return value.Undefined(), NewError(ErrInvalidOperation, "the object itself failed")
	default:
		return value.Undefined(), value.ErrUnknownMethod
	}
}

// recordingCallback answers `hooked`, fails loudly for `explode`, and declines
// everything else.
func recordingCallback(calls *[]string) UnknownMethodFunc {
	return func(_ *State, val value.Value, method string, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
		*calls = append(*calls, method)
		switch method {
		case "hooked":
			return value.FromString(fmt.Sprintf("hooked(%s,%d,%d)", val.Kind(), len(args), kwargs.Len())), nil
		case "explode":
			return value.Undefined(), NewError(ErrInvalidOperation, "callback refused")
		default:
			return value.Undefined(), NewError(ErrUnknownMethod, "not mine")
		}
	}
}

func renderWithHook(t *testing.T, source string, hook UnknownMethodFunc, ctx map[string]any) (string, error) {
	t.Helper()
	env := NewEnvironment()
	if hook != nil {
		env.SetUnknownMethodCallback(hook)
	}
	tmpl, err := env.TemplateFromNamedString("t.txt", source)
	if err != nil {
		t.Fatalf("parse %q: %v", source, err)
	}
	return tmpl.Render(ctx)
}

// TestUnknownMethodCallbackDispatchOrder covers the three rules the engine's
// method dispatch is made of, with a host object rather than a primitive.
func TestUnknownMethodCallbackDispatchOrder(t *testing.T) {
	obj := map[string]any{"obj": value.FromObject(&hostObject{})}

	t.Run("the object answers first", func(t *testing.T) {
		var calls []string
		got, err := renderWithHook(t, `{{ obj.own(1, 2) }}`, recordingCallback(&calls), obj)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if got != "own(2)" {
			t.Errorf("rendered %q, want %q", got, "own(2)")
		}
		if len(calls) != 0 {
			t.Errorf("the callback was consulted for a method the object answers: %v", calls)
		}
	})

	t.Run("a declined method reaches the callback", func(t *testing.T) {
		var calls []string
		got, err := renderWithHook(t, `{{ obj.hooked("x", k=1) }}`, recordingCallback(&calls), obj)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if want := "hooked(plain object,1,1)"; got != want {
			t.Errorf("rendered %q, want %q", got, want)
		}
		if len(calls) != 1 || calls[0] != "hooked" {
			t.Errorf("callback calls = %v, want exactly [hooked]", calls)
		}
	})

	t.Run("a callback error that is not ErrUnknownMethod propagates", func(t *testing.T) {
		var calls []string
		out, err := renderWithHook(t, `{{ obj.explode() }}`, recordingCallback(&calls), obj)
		if err == nil {
			t.Fatalf("rendered %q, want the callback's error", out)
		}
		var mjErr *Error
		if !errors.As(err, &mjErr) {
			t.Fatalf("error %T, want *minijinja.Error", err)
		}
		if mjErr.Kind != ErrInvalidOperation {
			t.Errorf("kind %v, want %v (the callback's own error, not unknown method)",
				mjErr.Kind, ErrInvalidOperation)
		}
	})

	t.Run("a declining callback leaves the engine's own error", func(t *testing.T) {
		var calls []string
		out, err := renderWithHook(t, `{{ obj.nope() }}`, recordingCallback(&calls), obj)
		if err == nil {
			t.Fatalf("rendered %q, want an unknown-method error", out)
		}
		var mjErr *Error
		if !errors.As(err, &mjErr) || mjErr.Kind != ErrUnknownMethod {
			t.Fatalf("error %v, want kind %v", err, ErrUnknownMethod)
		}
		if len(calls) != 1 || calls[0] != "nope" {
			t.Errorf("callback calls = %v, want exactly [nope]", calls)
		}
	})

	t.Run("no callback installed is still an unknown method", func(t *testing.T) {
		out, err := renderWithHook(t, `{{ obj.nope() }}`, nil, obj)
		if err == nil {
			t.Fatalf("rendered %q, want an unknown-method error", out)
		}
		var mjErr *Error
		if !errors.As(err, &mjErr) || mjErr.Kind != ErrUnknownMethod {
			t.Fatalf("error %v, want kind %v", err, ErrUnknownMethod)
		}
	})

	t.Run("the object's own error is not swallowed", func(t *testing.T) {
		var calls []string
		out, err := renderWithHook(t, `{{ obj.boom() }}`, recordingCallback(&calls), obj)
		if err == nil {
			t.Fatalf("rendered %q, want the object's error", out)
		}
		var mjErr *Error
		if !errors.As(err, &mjErr) || mjErr.Kind != ErrInvalidOperation {
			t.Fatalf("error %v, want kind %v", err, ErrInvalidOperation)
		}
		if len(calls) != 0 {
			t.Errorf("the callback was consulted after the object failed: %v", calls)
		}
	})
}

// TestUnknownMethodCallbackReachesEveryKind pins that the callback is offered
// values of any kind, not only the objects that have a method table — this is
// what lets a module implement methods on strings, maps and sequences.
func TestUnknownMethodCallbackReachesEveryKind(t *testing.T) {
	cases := []struct {
		source string
		kind   string
	}{
		{`{{ "x".hooked() }}`, "string"},
		{`{{ [1].hooked() }}`, "sequence"},
		{`{{ dict(a=1).hooked() }}`, "map"},
		{`{{ (42).hooked() }}`, "number"},
		{`{{ none.hooked() }}`, "none"},
		{`{{ obj.hooked() }}`, "plain object"},
	}
	ctx := map[string]any{"obj": value.FromObject(&hostObject{declineAll: true})}
	for _, tc := range cases {
		var calls []string
		got, err := renderWithHook(t, tc.source, recordingCallback(&calls), ctx)
		if err != nil {
			t.Errorf("%s: %v", tc.source, err)
			continue
		}
		if want := fmt.Sprintf("hooked(%s,0,0)", tc.kind); got != want {
			t.Errorf("%s = %q, want %q", tc.source, got, want)
		}
	}
}

// TestUnknownMethodCallbackNotConsultedForAttributes pins the other half of
// the dispatch split: a callable attribute is called, and a non-callable one
// is an invalid operation rather than an unknown method.
func TestUnknownMethodCallbackNotConsultedForAttributes(t *testing.T) {
	var calls []string
	out, err := renderWithHook(t, `{{ m.x() }}`, recordingCallback(&calls),
		map[string]any{"m": map[string]any{"x": 1}})
	if err == nil {
		t.Fatalf("rendered %q, want an invalid operation", out)
	}
	var mjErr *Error
	if !errors.As(err, &mjErr) || mjErr.Kind != ErrInvalidOperation {
		t.Fatalf("error %v, want kind %v", err, ErrInvalidOperation)
	}
	if len(calls) != 0 {
		t.Errorf("the callback was consulted for an existing attribute: %v", calls)
	}
}

// presenceObject is a generic host object that can hold an UNDEFINED value
// under a name that exists. It answers presence through the optional
// value.ObjectWithAttrLookup hook.
type presenceObject struct {
	entries map[string]value.Value
}

func (p *presenceObject) GetAttr(name string) value.Value {
	if v, ok := p.entries[name]; ok {
		return v
	}
	return value.Undefined()
}

func (p *presenceObject) LookupAttr(name string) (value.Value, bool) {
	v, ok := p.entries[name]
	if !ok {
		return value.Undefined(), false
	}
	return v, true
}

// TestMethodDispatchTestsPresenceNotValue pins the contract that decides
// whether a host's unknown-method callback runs at all.
//
// The engine's default `Object::call_method` is
// `if let Some(value) = self.get_value(...) { value.call(...) }`
// (value/object.rs:241-252). The branch is on the OPTION: a name that exists
// and holds undefined is called — and fails as "undefined is not callable" —
// where a name that does not exist is an unknown method. Deciding it on the
// retrieved value instead sent every present-but-undefined entry to the
// callback, which is a generic object contract error and not a pycompat one.
func TestMethodDispatchTestsPresenceNotValue(t *testing.T) {
	present := value.FromObject(&presenceObject{entries: map[string]value.Value{
		"x": value.Undefined(),
	}})
	absent := value.FromObject(&presenceObject{entries: map[string]value.Value{}})

	for _, tc := range []struct {
		name     string
		ctx      map[string]any
		wantKind ErrorKind
	}{
		{"a present entry holding undefined is called", map[string]any{"o": present}, ErrInvalidOperation},
		{"an absent name is an unknown method", map[string]any{"o": absent}, ErrUnknownMethod},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			out, err := renderWithHook(t, `{{ o.x() }}`, recordingCallback(&calls), tc.ctx)
			if err == nil {
				t.Fatalf("rendered %q, want an error", out)
			}
			var mjErr *Error
			if !errors.As(err, &mjErr) {
				t.Fatalf("error %T, want *minijinja.Error", err)
			}
			if mjErr.Kind != tc.wantKind {
				t.Fatalf("kind %v, want %v", mjErr.Kind, tc.wantKind)
			}
			// The callback is reached only through UnknownMethod, so it is
			// consulted for the absent name and for nothing else.
			wantCalls := 0
			if tc.wantKind == ErrUnknownMethod {
				wantCalls = 1
			}
			if len(calls) != wantCalls {
				t.Errorf("callback calls = %v, want %d", calls, wantCalls)
			}
		})
	}
}
