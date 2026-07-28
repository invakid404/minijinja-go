package minijinja

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/invakid404/minijinja-go/v2/value"
)

func TestVersion(t *testing.T) {
	if Version == "" {
		t.Error("Version should not be empty")
	}
}

func TestBasicRender(t *testing.T) {
	env := NewEnvironment()
	tmpl, err := env.TemplateFromString("Hello {{ name }}!")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	result, err := tmpl.Render(map[string]any{"name": "World"})
	if err != nil {
		t.Fatalf("render error: %v", err)
	}

	if result != "Hello World!" {
		t.Errorf("expected 'Hello World!', got %q", result)
	}
}

func TestVariableTypes(t *testing.T) {
	env := NewEnvironment()
	tmpl, err := env.TemplateFromString("{{ str }} {{ num }} {{ float }} {{ bool }}")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	result, err := tmpl.Render(map[string]any{
		"str":   "hello",
		"num":   42,
		"float": 3.14,
		"bool":  true,
	})
	if err != nil {
		t.Fatalf("render error: %v", err)
	}

	if result != "hello 42 3.14 true" {
		t.Errorf("expected 'hello 42 3.14 true', got %q", result)
	}
}

func TestForLoop(t *testing.T) {
	env := NewEnvironment()
	tmpl, err := env.TemplateFromString("{% for item in items %}{{ item }}{% endfor %}")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	result, err := tmpl.Render(map[string]any{
		"items": []string{"a", "b", "c"},
	})
	if err != nil {
		t.Fatalf("render error: %v", err)
	}

	if result != "abc" {
		t.Errorf("expected 'abc', got %q", result)
	}
}

func TestForLoopWithIndex(t *testing.T) {
	env := NewEnvironment()
	tmpl, err := env.TemplateFromString("{% for item in items %}{{ loop.index }}:{{ item }} {% endfor %}")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	result, err := tmpl.Render(map[string]any{
		"items": []string{"a", "b", "c"},
	})
	if err != nil {
		t.Fatalf("render error: %v", err)
	}

	if result != "1:a 2:b 3:c " {
		t.Errorf("expected '1:a 2:b 3:c ', got %q", result)
	}
}

func TestForLoopElse(t *testing.T) {
	env := NewEnvironment()
	tmpl, err := env.TemplateFromString("{% for item in items %}{{ item }}{% else %}empty{% endfor %}")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	result, err := tmpl.Render(map[string]any{
		"items": []string{},
	})
	if err != nil {
		t.Fatalf("render error: %v", err)
	}

	if result != "empty" {
		t.Errorf("expected 'empty', got %q", result)
	}
}

func TestIfCondition(t *testing.T) {
	env := NewEnvironment()
	tmpl, err := env.TemplateFromString("{% if show %}visible{% endif %}")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	result, err := tmpl.Render(map[string]any{"show": true})
	if err != nil {
		t.Fatalf("render error: %v", err)
	}
	if result != "visible" {
		t.Errorf("expected 'visible', got %q", result)
	}

	result, err = tmpl.Render(map[string]any{"show": false})
	if err != nil {
		t.Fatalf("render error: %v", err)
	}
	if result != "" {
		t.Errorf("expected '', got %q", result)
	}
}

func TestIfElse(t *testing.T) {
	env := NewEnvironment()
	tmpl, err := env.TemplateFromString("{% if show %}yes{% else %}no{% endif %}")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	result, err := tmpl.Render(map[string]any{"show": true})
	if err != nil {
		t.Fatalf("render error: %v", err)
	}
	if result != "yes" {
		t.Errorf("expected 'yes', got %q", result)
	}

	result, err = tmpl.Render(map[string]any{"show": false})
	if err != nil {
		t.Fatalf("render error: %v", err)
	}
	if result != "no" {
		t.Errorf("expected 'no', got %q", result)
	}
}

func TestIfElif(t *testing.T) {
	env := NewEnvironment()
	tmpl, err := env.TemplateFromString("{% if x == 1 %}one{% elif x == 2 %}two{% else %}other{% endif %}")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	tests := []struct {
		x        int
		expected string
	}{
		{1, "one"},
		{2, "two"},
		{3, "other"},
	}

	for _, test := range tests {
		result, err := tmpl.Render(map[string]any{"x": test.x})
		if err != nil {
			t.Fatalf("render error: %v", err)
		}
		if result != test.expected {
			t.Errorf("x=%d: expected %q, got %q", test.x, test.expected, result)
		}
	}
}

func TestArithmetic(t *testing.T) {
	env := NewEnvironment()

	tests := []struct {
		template string
		expected string
	}{
		{"{{ 1 + 2 }}", "3"},
		{"{{ 10 - 3 }}", "7"},
		{"{{ 4 * 5 }}", "20"},
		{"{{ 10 / 4 }}", "2.5"},
		{"{{ 10 // 4 }}", "2"},
		{"{{ 10 % 3 }}", "1"},
		{"{{ 2 ** 3 }}", "8"},
		{"{{ -5 }}", "-5"},
	}

	for _, test := range tests {
		tmpl, err := env.TemplateFromString(test.template)
		if err != nil {
			t.Fatalf("parse error for %q: %v", test.template, err)
		}
		result, err := tmpl.Render(nil)
		if err != nil {
			t.Fatalf("render error for %q: %v", test.template, err)
		}
		if result != test.expected {
			t.Errorf("%q: expected %q, got %q", test.template, test.expected, result)
		}
	}
}

func TestComparisons(t *testing.T) {
	env := NewEnvironment()

	tests := []struct {
		template string
		expected string
	}{
		{"{{ 1 == 1 }}", "true"},
		{"{{ 1 != 2 }}", "true"},
		{"{{ 1 < 2 }}", "true"},
		{"{{ 2 > 1 }}", "true"},
		{"{{ 1 <= 1 }}", "true"},
		{"{{ 2 >= 2 }}", "true"},
		{"{{ 1 == 2 }}", "false"},
	}

	for _, test := range tests {
		tmpl, err := env.TemplateFromString(test.template)
		if err != nil {
			t.Fatalf("parse error for %q: %v", test.template, err)
		}
		result, err := tmpl.Render(nil)
		if err != nil {
			t.Fatalf("render error for %q: %v", test.template, err)
		}
		if result != test.expected {
			t.Errorf("%q: expected %q, got %q", test.template, test.expected, result)
		}
	}
}

func TestLogicalOperators(t *testing.T) {
	env := NewEnvironment()

	tests := []struct {
		template string
		expected string
	}{
		{"{{ true and true }}", "true"},
		{"{{ true and false }}", "false"},
		{"{{ false or true }}", "true"},
		{"{{ false or false }}", "false"},
		{"{{ not true }}", "false"},
		{"{{ not false }}", "true"},
	}

	for _, test := range tests {
		tmpl, err := env.TemplateFromString(test.template)
		if err != nil {
			t.Fatalf("parse error for %q: %v", test.template, err)
		}
		result, err := tmpl.Render(nil)
		if err != nil {
			t.Fatalf("render error for %q: %v", test.template, err)
		}
		if result != test.expected {
			t.Errorf("%q: expected %q, got %q", test.template, test.expected, result)
		}
	}
}

func TestStringConcat(t *testing.T) {
	env := NewEnvironment()
	tmpl, err := env.TemplateFromString("{{ 'hello' ~ ' ' ~ 'world' }}")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	result, err := tmpl.Render(nil)
	if err != nil {
		t.Fatalf("render error: %v", err)
	}

	if result != "hello world" {
		t.Errorf("expected 'hello world', got %q", result)
	}
}

func TestFilters(t *testing.T) {
	env := NewEnvironment()

	tests := []struct {
		template string
		expected string
	}{
		{"{{ 'hello'|upper }}", "HELLO"},
		{"{{ 'HELLO'|lower }}", "hello"},
		{"{{ 'hello'|capitalize }}", "Hello"},
		{"{{ '  hello  '|trim }}", "hello"},
		{"{{ [1,2,3]|length }}", "3"},
		{"{{ [1,2,3]|first }}", "1"},
		{"{{ [1,2,3]|last }}", "3"},
		{"{{ [3,1,2]|sort|join(',') }}", "1,2,3"},
		{"{{ [1,2,3]|join('-') }}", "1-2-3"},
		{"{{ 'hello'|replace('l','x') }}", "hexxo"},
		{"{{ 5|abs }}", "5"},
		{"{{ -5|abs }}", "5"},
		{"{{ 3.7|int }}", "3"},
		{"{{ 3|float }}", "3.0"},
	}

	for _, test := range tests {
		tmpl, err := env.TemplateFromString(test.template)
		if err != nil {
			t.Fatalf("parse error for %q: %v", test.template, err)
		}
		result, err := tmpl.Render(nil)
		if err != nil {
			t.Fatalf("render error for %q: %v", test.template, err)
		}
		if result != test.expected {
			t.Errorf("%q: expected %q, got %q", test.template, test.expected, result)
		}
	}
}

func TestDefaultFilter(t *testing.T) {
	env := NewEnvironment()

	tests := []struct {
		template string
		ctx      map[string]any
		expected string
	}{
		{"{{ x|default('fallback') }}", nil, "fallback"},
		{"{{ x|default('fallback') }}", map[string]any{"x": "value"}, "value"},
		{"{{ x|d('fallback') }}", nil, "fallback"},
	}

	for _, test := range tests {
		tmpl, err := env.TemplateFromString(test.template)
		if err != nil {
			t.Fatalf("parse error for %q: %v", test.template, err)
		}
		result, err := tmpl.Render(test.ctx)
		if err != nil {
			t.Fatalf("render error for %q: %v", test.template, err)
		}
		if result != test.expected {
			t.Errorf("%q: expected %q, got %q", test.template, test.expected, result)
		}
	}
}

func TestTests(t *testing.T) {
	env := NewEnvironment()

	tests := []struct {
		template string
		ctx      map[string]any
		expected string
	}{
		{"{{ x is defined }}", map[string]any{"x": 1}, "true"},
		{"{{ y is defined }}", map[string]any{"x": 1}, "false"},
		{"{{ x is undefined }}", map[string]any{"x": 1}, "false"},
		{"{{ y is undefined }}", map[string]any{"x": 1}, "true"},
		{"{{ none is none }}", nil, "true"},
		{"{{ 1 is none }}", nil, "false"},
		{"{{ 3 is odd }}", nil, "true"},
		{"{{ 4 is even }}", nil, "true"},
		{"{{ 10 is divisibleby(5) }}", nil, "true"},
		{"{{ 10 is divisibleby(3) }}", nil, "false"},
	}

	for _, test := range tests {
		tmpl, err := env.TemplateFromString(test.template)
		if err != nil {
			t.Fatalf("parse error for %q: %v", test.template, err)
		}
		result, err := tmpl.Render(test.ctx)
		if err != nil {
			t.Fatalf("render error for %q: %v", test.template, err)
		}
		if result != test.expected {
			t.Errorf("%q: expected %q, got %q", test.template, test.expected, result)
		}
	}
}

func TestOperatorAliasTests(t *testing.T) {
	env := NewEnvironment()

	aliases := []string{
		"equalto",
		"==",
		"ne",
		"!=",
		"lessthan",
		"<",
		"le",
		"<=",
		"greaterthan",
		">",
		"ge",
		">=",
	}

	for _, alias := range aliases {
		tmpl, err := env.TemplateFromString("{{ '" + alias + "' is test }}")
		if err != nil {
			t.Fatalf("parse error for %q: %v", alias, err)
		}
		result, err := tmpl.Render(nil)
		if err != nil {
			t.Fatalf("render error for %q: %v", alias, err)
		}
		if result != "true" {
			t.Errorf("alias %q: expected \"true\", got %q", alias, result)
		}
	}
}

func TestInOperator(t *testing.T) {
	env := NewEnvironment()

	tests := []struct {
		template string
		ctx      map[string]any
		expected string
	}{
		{"{{ 'a' in 'abc' }}", nil, "true"},
		{"{{ 'd' in 'abc' }}", nil, "false"},
		{"{{ 1 in [1,2,3] }}", nil, "true"},
		{"{{ 4 in [1,2,3] }}", nil, "false"},
	}

	for _, test := range tests {
		tmpl, err := env.TemplateFromString(test.template)
		if err != nil {
			t.Fatalf("parse error for %q: %v", test.template, err)
		}
		result, err := tmpl.Render(test.ctx)
		if err != nil {
			t.Fatalf("render error for %q: %v", test.template, err)
		}
		if result != test.expected {
			t.Errorf("%q: expected %q, got %q", test.template, test.expected, result)
		}
	}
}

func TestAttributeAccess(t *testing.T) {
	env := NewEnvironment()
	tmpl, err := env.TemplateFromString("{{ user.name }} is {{ user.age }}")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	result, err := tmpl.Render(map[string]any{
		"user": map[string]any{
			"name": "Alice",
			"age":  30,
		},
	})
	if err != nil {
		t.Fatalf("render error: %v", err)
	}

	if result != "Alice is 30" {
		t.Errorf("expected 'Alice is 30', got %q", result)
	}
}

func TestIndexAccess(t *testing.T) {
	env := NewEnvironment()
	tmpl, err := env.TemplateFromString("{{ items[0] }} {{ items[1] }} {{ items[-1] }}")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	result, err := tmpl.Render(map[string]any{
		"items": []string{"a", "b", "c"},
	})
	if err != nil {
		t.Fatalf("render error: %v", err)
	}

	if result != "a b c" {
		t.Errorf("expected 'a b c', got %q", result)
	}
}

func TestRangeFunction(t *testing.T) {
	env := NewEnvironment()

	tests := []struct {
		template string
		expected string
	}{
		{"{% for i in range(3) %}{{ i }}{% endfor %}", "012"},
		{"{% for i in range(1, 4) %}{{ i }}{% endfor %}", "123"},
		{"{% for i in range(0, 6, 2) %}{{ i }}{% endfor %}", "024"},
	}

	for _, test := range tests {
		tmpl, err := env.TemplateFromString(test.template)
		if err != nil {
			t.Fatalf("parse error for %q: %v", test.template, err)
		}
		result, err := tmpl.Render(nil)
		if err != nil {
			t.Fatalf("render error for %q: %v", test.template, err)
		}
		if result != test.expected {
			t.Errorf("%q: expected %q, got %q", test.template, test.expected, result)
		}
	}
}

func TestSetStatement(t *testing.T) {
	env := NewEnvironment()
	tmpl, err := env.TemplateFromString("{% set x = 5 %}{{ x }}")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	result, err := tmpl.Render(nil)
	if err != nil {
		t.Fatalf("render error: %v", err)
	}

	if result != "5" {
		t.Errorf("expected '5', got %q", result)
	}
}

func TestWithBlock(t *testing.T) {
	env := NewEnvironment()
	tmpl, err := env.TemplateFromString("{% with x = 5, y = 10 %}{{ x + y }}{% endwith %}")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	result, err := tmpl.Render(nil)
	if err != nil {
		t.Fatalf("render error: %v", err)
	}

	if result != "15" {
		t.Errorf("expected '15', got %q", result)
	}
}

func TestTernaryExpression(t *testing.T) {
	env := NewEnvironment()
	tmpl, err := env.TemplateFromString("{{ 'yes' if x else 'no' }}")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	result, err := tmpl.Render(map[string]any{"x": true})
	if err != nil {
		t.Fatalf("render error: %v", err)
	}
	if result != "yes" {
		t.Errorf("expected 'yes', got %q", result)
	}

	result, err = tmpl.Render(map[string]any{"x": false})
	if err != nil {
		t.Fatalf("render error: %v", err)
	}
	if result != "no" {
		t.Errorf("expected 'no', got %q", result)
	}
}

func TestHTMLEscaping(t *testing.T) {
	env := NewEnvironment()
	tmpl, err := env.TemplateFromNamedString("test.html", "{{ content }}")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	result, err := tmpl.Render(map[string]any{"content": "<script>alert('xss')</script>"})
	if err != nil {
		t.Fatalf("render error: %v", err)
	}

	if !strings.Contains(result, "&lt;") {
		t.Errorf("expected HTML escaping, got %q", result)
	}
}

func TestSafeFilter(t *testing.T) {
	env := NewEnvironment()
	tmpl, err := env.TemplateFromNamedString("test.html", "{{ content|safe }}")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	result, err := tmpl.Render(map[string]any{"content": "<b>bold</b>"})
	if err != nil {
		t.Fatalf("render error: %v", err)
	}

	if result != "<b>bold</b>" {
		t.Errorf("expected '<b>bold</b>', got %q", result)
	}
}

func TestMacro(t *testing.T) {
	env := NewEnvironment()
	tmpl, err := env.TemplateFromString(`{% macro greet(name) %}Hello {{ name }}!{% endmacro %}{{ greet("World") }}`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	result, err := tmpl.Render(nil)
	if err != nil {
		t.Fatalf("render error: %v", err)
	}

	if result != "Hello World!" {
		t.Errorf("expected 'Hello World!', got %q", result)
	}
}

func TestMacroWithDefault(t *testing.T) {
	env := NewEnvironment()
	tmpl, err := env.TemplateFromString(`{% macro greet(name="Guest") %}Hello {{ name }}!{% endmacro %}{{ greet() }}`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	result, err := tmpl.Render(nil)
	if err != nil {
		t.Fatalf("render error: %v", err)
	}

	if result != "Hello Guest!" {
		t.Errorf("expected 'Hello Guest!', got %q", result)
	}
}

// TestInclude: this build has no `include` statement. The feature that carries it
// (multi_template) is not in BAML's engine feature set, so the template does not
// compile -- see internal/parser/features.go, PATCHES.md #2 and the
// exhaustive gate coverage in feature_gate_test.go.
func TestInclude(t *testing.T) {
	assertGatedStatement(t, "include", `{% include "header.html" %}`)
}

func TestSlicing(t *testing.T) {
	env := NewEnvironment()

	tests := []struct {
		template string
		expected string
	}{
		{"{{ 'hello'[1:3] }}", "el"},
		{"{{ 'hello'[:3] }}", "hel"},
		{"{{ 'hello'[2:] }}", "llo"},
		{"{{ [1,2,3,4,5][1:4] }}", "[2, 3, 4]"},
	}

	for _, test := range tests {
		tmpl, err := env.TemplateFromString(test.template)
		if err != nil {
			t.Fatalf("parse error for %q: %v", test.template, err)
		}
		result, err := tmpl.Render(nil)
		if err != nil {
			t.Fatalf("render error for %q: %v", test.template, err)
		}
		if result != test.expected {
			t.Errorf("%q: expected %q, got %q", test.template, test.expected, result)
		}
	}
}

func TestDictLiteral(t *testing.T) {
	env := NewEnvironment()
	tmpl, err := env.TemplateFromString(`{{ {'a': 1, 'b': 2}.a }}`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	result, err := tmpl.Render(nil)
	if err != nil {
		t.Fatalf("render error: %v", err)
	}

	if result != "1" {
		t.Errorf("expected '1', got %q", result)
	}
}

func TestNestedForLoop(t *testing.T) {
	env := NewEnvironment()
	tmpl, err := env.TemplateFromString(`{% for row in rows %}{% for col in row %}{{ col }}{% endfor %},{% endfor %}`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	result, err := tmpl.Render(map[string]any{
		"rows": [][]int{{1, 2}, {3, 4}},
	})
	if err != nil {
		t.Fatalf("render error: %v", err)
	}

	if result != "12,34," {
		t.Errorf("expected '12,34,', got %q", result)
	}
}

func TestForLoopFilter(t *testing.T) {
	env := NewEnvironment()
	tmpl, err := env.TemplateFromString(`{% for x in items if x > 2 %}{{ x }}{% endfor %}`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	result, err := tmpl.Render(map[string]any{
		"items": []int{1, 2, 3, 4, 5},
	})
	if err != nil {
		t.Fatalf("render error: %v", err)
	}

	if result != "345" {
		t.Errorf("expected '345', got %q", result)
	}
}

// --- Template Inheritance Tests ---

// TestTemplateExtends: this build has no `extends` statement. The feature that carries it
// (multi_template) is not in BAML's engine feature set, so the template does not
// compile -- see internal/parser/features.go, PATCHES.md #2 and the
// exhaustive gate coverage in feature_gate_test.go.
func TestTemplateExtends(t *testing.T) {
	assertGatedStatement(t, "extends", `{% extends "base.html" %}{% block content %}Hello World{% endblock %}`)
}

// TestTemplateExtendsWithSuper: this build has no `extends` statement. The feature that carries it
// (multi_template) is not in BAML's engine feature set, so the template does not
// compile -- see internal/parser/features.go, PATCHES.md #2 and the
// exhaustive gate coverage in feature_gate_test.go.
func TestTemplateExtendsWithSuper(t *testing.T) {
	assertGatedStatement(t, "extends", `{% extends "base.html" %}{% block content %}{{ super() }}:CHILD{% endblock %}`)
}

// TestTemplateExtendsMultipleLevels: this build has no `extends` statement. The feature that carries it
// (multi_template) is not in BAML's engine feature set, so the template does not
// compile -- see internal/parser/features.go, PATCHES.md #2 and the
// exhaustive gate coverage in feature_gate_test.go.
func TestTemplateExtendsMultipleLevels(t *testing.T) {
	assertGatedStatement(t, "extends", `{% extends "middle.html" %}{% block content %}CHILD{% endblock %}`)
}

// TestTemplateExtendsWithVariable: this build has no `extends` statement. The feature that carries it
// (multi_template) is not in BAML's engine feature set, so the template does not
// compile -- see internal/parser/features.go, PATCHES.md #2 and the
// exhaustive gate coverage in feature_gate_test.go.
func TestTemplateExtendsWithVariable(t *testing.T) {
	assertGatedStatement(t, "extends", `{% extends "base.html" %}{% block name %}{{ name }}{% endblock %}`)
}

// --- Import Tests ---

// TestImport: this build has no `import` statement. The feature that carries it
// (multi_template) is not in BAML's engine feature set, so the template does not
// compile -- see internal/parser/features.go, PATCHES.md #2 and the
// exhaustive gate coverage in feature_gate_test.go.
func TestImport(t *testing.T) {
	assertGatedStatement(t, "import", `{% import "forms.html" as forms %}{{ forms.input("test") }}`)
}

// TestFromImport: this build has no `from` statement. The feature that carries it
// (multi_template) is not in BAML's engine feature set, so the template does not
// compile -- see internal/parser/features.go, PATCHES.md #2 and the
// exhaustive gate coverage in feature_gate_test.go.
func TestFromImport(t *testing.T) {
	assertGatedStatement(t, "from", `{% from "forms.html" import input, button %}{{ input("test") }}`)
}

// TestFromImportWithAlias: this build has no `from` statement. The feature that carries it
// (multi_template) is not in BAML's engine feature set, so the template does not
// compile -- see internal/parser/features.go, PATCHES.md #2 and the
// exhaustive gate coverage in feature_gate_test.go.
func TestFromImportWithAlias(t *testing.T) {
	assertGatedStatement(t, "from", `{% from "forms.html" import input as inp %}{{ inp("test") }}`)
}

// --- Loop Recursion Tests ---

func TestLoopRecursion(t *testing.T) {
	env := NewEnvironment()
	// The recursive loop re-applies the loop body to nested children
	tmpl, err := env.TemplateFromString(`{% for item in items recursive %}{{ item.name }}{% if item.children %}({{ loop(item.children) }}){% endif %}{% endfor %}`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	result, err := tmpl.Render(map[string]any{
		"items": []map[string]any{
			{"name": "A", "children": []map[string]any{
				{"name": "A1", "children": nil},
				{"name": "A2", "children": nil},
			}},
			{"name": "B", "children": nil},
		},
	})
	if err != nil {
		t.Fatalf("render error: %v", err)
	}

	if result != "A(A1A2)B" {
		t.Errorf("expected 'A(A1A2)B', got %q", result)
	}
}

// --- Advanced Filters Tests ---

func TestTojsonFilter(t *testing.T) {
	env := NewEnvironment()

	tests := []struct {
		template string
		ctx      map[string]any
		expected string
	}{
		{`{{ value|tojson }}`, map[string]any{"value": "hello"}, `"hello"`},
		{`{{ value|tojson }}`, map[string]any{"value": 42}, `42`},
		{`{{ value|tojson }}`, map[string]any{"value": []int{1, 2, 3}}, `[1,2,3]`},
		{`{{ value|tojson }}`, map[string]any{"value": map[string]any{"a": 1}}, `{"a":1}`},
	}

	for _, test := range tests {
		tmpl, err := env.TemplateFromString(test.template)
		if err != nil {
			t.Fatalf("parse error for %q: %v", test.template, err)
		}
		result, err := tmpl.Render(test.ctx)
		if err != nil {
			t.Fatalf("render error for %q: %v", test.template, err)
		}
		if result != test.expected {
			t.Errorf("%q: expected %q, got %q", test.template, test.expected, result)
		}
	}
}

// TestGoOnlyBuiltinsWithdrawn pins the withdrawal of the five names the Go port
// shipped and BAML's engine build does not have (defaults.go:13-33). Each must
// produce the engine's own unknown-filter/test/function error, never an answer.
//
// Corpus: err/go-only-urlencode-filter, err/go-only-containing-test,
// err/go-only-cycler-function, err/go-only-joiner-function,
// err/go-only-lipsum-function.
func TestGoOnlyBuiltinsWithdrawn(t *testing.T) {
	cases := []struct {
		template string
		kind     ErrorKind
	}{
		{`{{ "a b"|urlencode }}`, ErrUnknownFilter},
		{`{{ "foobar" is containing("oob") }}`, ErrUnknownTest},
		{`{{ cycler("a", "b") }}`, ErrUnknownFunction},
		{`{{ joiner(", ") }}`, ErrUnknownFunction},
		{`{{ lipsum(1) }}`, ErrUnknownFunction},
	}

	env := NewEnvironment()
	for _, tc := range cases {
		tmpl, err := env.TemplateFromString(tc.template)
		if err != nil {
			t.Fatalf("parse error for %q: %v", tc.template, err)
		}
		out, err := tmpl.Render(nil)
		if err == nil {
			t.Errorf("%q: rendered %q, want %v", tc.template, out, tc.kind)
			continue
		}
		var mjErr *Error
		if !errors.As(err, &mjErr) {
			t.Errorf("%q: error %T, want *minijinja.Error", tc.template, err)
			continue
		}
		if mjErr.Kind != tc.kind {
			t.Errorf("%q: error kind %v, want %v", tc.template, mjErr.Kind, tc.kind)
		}
	}
}

// --- Callable Objects Tests ---

// testCycler is the shape the withdrawn `cycler` global used to provide: an
// object whose attribute is a callable. The engine capability is still
// supported for host objects, so it is still covered.
type testCycler struct {
	items []string
	index int
}

func (c *testCycler) GetAttr(name string) value.Value {
	if name != "next" {
		return value.Undefined()
	}
	return value.FromCallable(callableFunc(func() value.Value {
		rv := c.items[c.index]
		c.index = (c.index + 1) % len(c.items)
		return value.FromString(rv)
	}))
}

type callableFunc func() value.Value

func (f callableFunc) Call(value.State, []value.Value, map[string]value.Value) (value.Value, error) {
	return f(), nil
}

func TestCallableObject(t *testing.T) {
	env := NewEnvironment()

	tmpl, err := env.TemplateFromString(`{{ c.next() }} {{ c.next() }} {{ c.next() }}`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	result, err := tmpl.Render(map[string]any{
		"c": value.FromObject(&testCycler{items: []string{"odd", "even"}}),
	})
	if err != nil {
		t.Fatalf("render error: %v", err)
	}

	if result != "odd even odd" {
		t.Errorf("expected 'odd even odd', got %q", result)
	}
}

func TestCallableValue(t *testing.T) {
	env := NewEnvironment()
	first := true
	env.AddFunction("joiner", func(*State, []value.Value, map[string]value.Value) (value.Value, error) {
		if first {
			first = false
			return value.FromString(""), nil
		}
		return value.FromString(", "), nil
	})

	tmpl, err := env.TemplateFromString(`{% for item in items %}{{ joiner() }}{{ item }}{% endfor %}`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	result, err := tmpl.Render(map[string]any{
		"items": []string{"a", "b", "c"},
	})
	if err != nil {
		t.Fatalf("render error: %v", err)
	}

	if result != "a, b, c" {
		t.Errorf("expected 'a, b, c', got %q", result)
	}
}

// --- Namespace Tests ---

func TestNamespace(t *testing.T) {
	env := NewEnvironment()

	tmpl, err := env.TemplateFromString(`{% set ns = namespace(count=0) %}{% for i in range(3) %}{% set ns.count = ns.count + 1 %}{% endfor %}{{ ns.count }}`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	result, err := tmpl.Render(nil)
	if err != nil {
		t.Fatalf("render error: %v", err)
	}

	if result != "3" {
		t.Errorf("expected '3', got %q", result)
	}
}

// --- Context Tests ---

func TestRenderCtx(t *testing.T) {
	env := NewEnvironment()

	// Add a function that accesses the context
	type ctxKey string
	env.AddFunction("get_value", func(state *State, args []Value, kwargs map[string]Value) (Value, error) {
		ctx := state.Context()
		if v, ok := ctx.Value(ctxKey("test")).(string); ok {
			return value.FromString(v), nil
		}
		return value.FromString("not found"), nil
	})

	tmpl, err := env.TemplateFromString(`{{ get_value() }}`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	// Test with context value
	ctx := context.WithValue(context.Background(), ctxKey("test"), "hello from context")
	result, err := tmpl.RenderCtx(ctx, nil)
	if err != nil {
		t.Fatalf("render error: %v", err)
	}

	if result != "hello from context" {
		t.Errorf("expected 'hello from context', got %q", result)
	}

	// Test without context value (using Render)
	result2, err := tmpl.Render(nil)
	if err != nil {
		t.Fatalf("render error: %v", err)
	}

	if result2 != "not found" {
		t.Errorf("expected 'not found', got %q", result2)
	}
}

func TestStateAccessors(t *testing.T) {
	env := NewEnvironment()

	var capturedState *State
	env.AddFunction("capture_state", func(state *State, args []Value, kwargs map[string]Value) (Value, error) {
		capturedState = state
		return value.FromString("ok"), nil
	})

	tmpl, err := env.TemplateFromNamedString("test.html", `{{ capture_state() }}`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	ctx := context.Background()
	_, err = tmpl.RenderCtx(ctx, nil)
	if err != nil {
		t.Fatalf("render error: %v", err)
	}

	// Test State accessors
	if capturedState.Name() != "test.html" {
		t.Errorf("expected template name 'test.html', got %q", capturedState.Name())
	}

	if capturedState.Env() != env {
		t.Error("State.Env() should return the environment")
	}

	if capturedState.Context() != ctx {
		t.Error("State.Context() should return the context")
	}

	if capturedState.AutoEscape() != AutoEscapeHTML {
		t.Error("State.AutoEscape() should be HTML for .html files")
	}
}

// --- One-Shot Iterator Tests ---

func TestOneShotIterator(t *testing.T) {
	env := NewEnvironment()

	// Create a one-shot iterator
	iter := value.MakeOneShotIterator(func(yield func(value.Value) bool) {
		for i := 0; i < 5; i++ {
			if !yield(value.FromInt(int64(i))) {
				return
			}
		}
	})

	env.AddGlobal("one_shot", iter)

	// Test that it can be iterated
	tmpl, err := env.TemplateFromString(`{% for item in one_shot %}[{{ item }}]{% endfor %}`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	result, err := tmpl.Render(nil)
	if err != nil {
		t.Fatalf("render error: %v", err)
	}

	expected := "[0][1][2][3][4]"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}

	// Test that loop.length is unknown (uses default)
	iter2 := value.MakeOneShotIterator(func(yield func(value.Value) bool) {
		for i := 0; i < 3; i++ {
			if !yield(value.FromInt(int64(i))) {
				return
			}
		}
	})

	env2 := NewEnvironment()
	env2.AddGlobal("one_shot", iter2)

	tmpl2, err := env2.TemplateFromString(`{% for item in one_shot %}- {{ item }}: {{ loop.length|default("?") }}
{% endfor %}`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	result2, err := tmpl2.Render(nil)
	if err != nil {
		t.Fatalf("render error: %v", err)
	}

	expected2 := `- 0: ?
- 1: ?
- 2: ?
`
	if result2 != expected2 {
		t.Errorf("expected %q, got %q", expected2, result2)
	}
}

// TestOneShotIteratorPartialConsumption: partial consumption used to be driven
// by {% break %}. loop_controls is not in BAML's engine feature set, so there is
// no break statement in this build and a loop always drains its iterator. The
// test keeps both halves of the original claim: break does not compile, and a
// one-shot iterator that a loop ran to completion yields nothing afterwards.
//
// See internal/parser/features.go, PATCHES.md #2, feature_gate_test.go.
func TestOneShotIteratorPartialConsumption(t *testing.T) {
	assertGatedStatement(t, "break",
		`{% for item in one_shot %}{{ item }}{% if item == 1 %}{% break %}{% endif %}{% endfor %}`)

	env := NewEnvironment()
	env.AddGlobal("one_shot", value.MakeOneShotIterator(func(yield func(value.Value) bool) {
		for i := 0; i < 5; i++ {
			if !yield(value.FromInt(int64(i))) {
				return
			}
		}
	}))

	tmpl, err := env.TemplateFromString(
		`{% for item in one_shot %}{{ item }}{% endfor %}` +
			`|{% for item in one_shot %}{{ item }}{% endfor %}`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	result, err := tmpl.Render(nil)
	if err != nil {
		t.Fatalf("render error: %v", err)
	}

	if expected := "01234|"; result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestOneShotIteratorConsumed(t *testing.T) {
	env := NewEnvironment()

	// Create a one-shot iterator
	iter := value.MakeOneShotIterator(func(yield func(value.Value) bool) {
		for i := 0; i < 3; i++ {
			if !yield(value.FromInt(int64(i))) {
				return
			}
		}
	})

	env.AddGlobal("one_shot", iter)

	// Test that second iteration yields nothing after full consumption
	tmpl, err := env.TemplateFromString(
		`{% for item in one_shot %}{{ item }}{% endfor %}` +
			`|{% for item in one_shot %}{{ item }}{% endfor %}`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	result, err := tmpl.Render(nil)
	if err != nil {
		t.Fatalf("render error: %v", err)
	}

	// First loop: 0, 1, 2
	// Second loop: nothing (consumed)
	expected := "012|"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestOneShotIteratorString(t *testing.T) {
	env := NewEnvironment()

	iter := value.MakeOneShotIterator(func(yield func(value.Value) bool) {
		yield(value.FromInt(1))
	})

	env.AddGlobal("one_shot", iter)

	tmpl, err := env.TemplateFromString(`{{ one_shot }}`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	result, err := tmpl.Render(nil)
	if err != nil {
		t.Fatalf("render error: %v", err)
	}

	if result != "<iterator>" {
		t.Errorf("expected '<iterator>', got %q", result)
	}
}
