package minijinja

import (
	"bytes"
	"testing"

	"github.com/invakid404/minijinja-go/v2/value"
)

func TestEvalToState(t *testing.T) {
	env := NewEnvironment()

	// No {% block %} here: blocks belong to multi_template, which BAML does
	// not enable, so this build has no block statement at all (see
	// internal/parser/features.go and PATCHES.md #2). The state API is still
	// exercised for macros, sets, lookups and exports.
	err := env.AddTemplate("test.html", `
{% macro greet(name) %}Hello {{ name }}!{% endmacro %}
{% set version = "1.0" %}
`)
	if err != nil {
		t.Fatal(err)
	}

	tmpl, err := env.GetTemplate("test.html")
	if err != nil {
		t.Fatal(err)
	}

	state, err := tmpl.EvalToState(map[string]any{
		"user": "John",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Test Name (State returns the template name)
	if state.Name() != "test.html" {
		t.Errorf("expected name 'test.html', got %q", state.Name())
	}

	// Test CallMacro
	result, err := state.CallMacro("greet", value.FromString("World"))
	if err != nil {
		t.Fatal(err)
	}
	if result.String() != "Hello World!" {
		t.Errorf("expected 'Hello World!', got %q", result.String())
	}

	// Test Lookup
	ver := state.Lookup("version")
	if v, ok := ver.AsString(); !ok || v != "1.0" {
		t.Errorf("expected version '1.0', got %v", ver)
	}

	user := state.Lookup("user")
	if v, ok := user.AsString(); !ok || v != "John" {
		t.Errorf("expected user 'John', got %v", user)
	}

	// Test Exports
	exports := state.Exports()
	if _, ok := exports["version"]; !ok {
		t.Error("expected 'version' in exports")
	}
	if _, ok := exports["greet"]; !ok {
		t.Error("expected 'greet' macro in exports")
	}

	// Test BlockNames: no statement in this build can declare one.
	if blocks := state.BlockNames(); len(blocks) != 0 {
		t.Errorf("expected no blocks, got %v", blocks)
	}

	// Test MacroNames
	macros := state.MacroNames()
	found := false
	for _, m := range macros {
		if m == "greet" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'greet' in macro names, got %v", macros)
	}
}

// TestEvalToStateInheritance: template inheritance does not exist in this
// build. `extends` and `block` are multi_template statements, which BAML's
// engine feature set omits, so the child template below does not compile --
// see internal/parser/features.go, PATCHES.md #2 and feature_gate_test.go.
func TestEvalToStateInheritance(t *testing.T) {
	assertGatedStatement(t, "extends", `{% extends "base.html" %}{% block title %}Child Page{% endblock %}`)
}

func TestRenderToWrite(t *testing.T) {
	env := NewEnvironment()

	tmpl, err := env.TemplateFromString("Hello {{ name }}!")
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	err = tmpl.RenderToWrite(map[string]any{"name": "World"}, &buf)
	if err != nil {
		t.Fatal(err)
	}

	if buf.String() != "Hello World!" {
		t.Errorf("expected 'Hello World!', got %q", buf.String())
	}
}

func TestSetFormatter(t *testing.T) {
	env := NewEnvironment()

	// Set formatter that treats None as empty
	env.SetFormatter(func(state *State, val value.Value, escape func(string) string) string {
		if val.IsNone() {
			return ""
		}
		s := val.String()
		if !val.IsSafe() {
			s = escape(s)
		}
		return s
	})

	tmpl, err := env.TemplateFromString("Value: [{{ val }}]")
	if err != nil {
		t.Fatal(err)
	}

	// Test with None
	result, err := tmpl.Render(map[string]any{"val": nil})
	if err != nil {
		t.Fatal(err)
	}
	if result != "Value: []" {
		t.Errorf("expected 'Value: []', got %q", result)
	}

	// Test with actual value
	result, err = tmpl.Render(map[string]any{"val": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if result != "Value: [hello]" {
		t.Errorf("expected 'Value: [hello]', got %q", result)
	}
}

func TestCallMacroKw(t *testing.T) {
	env := NewEnvironment()

	err := env.AddTemplate("test.html", `
{% macro input(name, value="", type="text") -%}
<input name="{{ name }}" value="{{ value }}" type="{{ type }}">
{%- endmacro %}
`)
	if err != nil {
		t.Fatal(err)
	}

	tmpl, err := env.GetTemplate("test.html")
	if err != nil {
		t.Fatal(err)
	}

	state, err := tmpl.EvalToState(nil)
	if err != nil {
		t.Fatal(err)
	}

	// Call with kwargs
	kwargs := value.NewOrderedMap(1)
	kwargs.Set("type", value.FromString("email"))
	result, err := state.CallMacroKw("input",
		[]value.Value{value.FromString("email")},
		kwargs,
	)
	if err != nil {
		t.Fatal(err)
	}

	expected := `<input name="email" value="" type="email">`
	if result.String() != expected {
		t.Errorf("expected %q, got %q", expected, result.String())
	}
}
