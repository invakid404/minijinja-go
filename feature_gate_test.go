package minijinja

import (
	"fmt"
	"strings"
	"testing"

	"github.com/invakid404/minijinja-go/v2/internal/parser"
)

// The statements below are carried by engine features BAML does not enable
// (`multi_template`, `loop_controls`; see internal/parser/features.go). In
// BAML's Rust build those keywords are compiled out of the parser, so a
// template using one fails to compile with `unknown statement <name>` before
// anything can be resolved or loaded. This fork does the same, and these tests
// are the positive proof of it.
//
// Differential rows: tmpl/negative-* in oracle/corpus/template.json.
// Delta: PATCHES.md #2.

// assertGatedStatement renders nothing: it requires that source fails to
// compile with the engine's exact "unknown statement" syntax error.
func assertGatedStatement(t *testing.T, keyword, source string) {
	t.Helper()

	env := NewEnvironment()
	_, err := env.TemplateFromNamedString("gated.txt", source)
	if err == nil {
		t.Fatalf("%q compiled, but %q is not a statement in this build", source, keyword)
	}
	assertGateError(t, keyword, err)

	// AddTemplate is the other entry point, and it must agree: a template that
	// cannot be compiled must never make it into the environment.
	if err := env.AddTemplate("gated.txt", source); err == nil {
		t.Fatalf("AddTemplate accepted %q", source)
	}
	if _, err := env.GetTemplate("gated.txt"); err == nil {
		t.Fatalf("a template that failed to compile was still registered")
	}
}

func assertGateError(t *testing.T, keyword string, err error) {
	t.Helper()

	mjErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("compile error is %T, not *minijinja.Error; an external consumer cannot classify it", err)
	}
	if mjErr.Kind != ErrSyntax {
		t.Fatalf("expected kind %v, got %v (%v)", ErrSyntax, mjErr.Kind, mjErr)
	}
	want := fmt.Sprintf("unknown statement %s", keyword)
	if !strings.Contains(mjErr.Message, want) {
		t.Fatalf("expected message to contain %q, got %q", want, mjErr.Message)
	}
}

func TestGatedStatementsAreNotStatements(t *testing.T) {
	sources := map[string][]string{
		"block": {
			`{% block body %}x{% endblock %}`,
			`{% block body %}{{ super() }}{% endblock %}`,
		},
		"extends": {
			`{% extends "base.txt" %}`,
			`{% extends "base.txt" %}{% block body %}x{% endblock %}`,
		},
		"include": {
			`{% include "other.txt" %}`,
			`{% include "other.txt" ignore missing %}`,
			`{% include ["a.txt", "b.txt"] %}`,
		},
		"import": {`{% import "m.txt" as m %}`},
		"from":   {`{% from "m.txt" import thing %}`},
		"break":  {`{% for x in [1] %}{% break %}{% endfor %}`},
		"continue": {
			`{% for x in [1] %}{% continue %}{% endfor %}`,
		},
	}

	// Every gated keyword must be covered here, so adding one to the gate
	// without proving it is a test failure rather than a silent omission.
	for keyword := range parser.GatedStatements() {
		if _, ok := sources[keyword]; !ok {
			t.Errorf("gated statement %q has no test case", keyword)
		}
	}

	for keyword, cases := range sources {
		if _, gated := parser.GatedStatements()[keyword]; !gated {
			t.Errorf("%q is asserted here but is not in the gate", keyword)
			continue
		}
		for _, source := range cases {
			t.Run(keyword+" "+source, func(t *testing.T) {
				assertGatedStatement(t, keyword, source)
			})
		}
	}
}

// TestGatedClosingTagsAreNotStatements covers the other half: the closing tags
// of gated statements must be unknown statements too, not a different error.
func TestGatedClosingTagsAreNotStatements(t *testing.T) {
	for _, tc := range []struct{ keyword, source string }{
		{"endblock", `x{% endblock %}`},
	} {
		t.Run(tc.source, func(t *testing.T) {
			env := NewEnvironment()
			_, err := env.TemplateFromNamedString("gated.txt", tc.source)
			if err == nil {
				t.Fatalf("%q compiled", tc.source)
			}
			assertGateError(t, tc.keyword, err)
		})
	}
}

// TestNoLoaderIsReachableFromTemplateSyntax is the negative control the scope
// asks for: with the statements gated, a configured loader must never be
// consulted while rendering a template, because nothing in template syntax can
// ask for a second template.
func TestNoLoaderIsReachableFromTemplateSyntax(t *testing.T) {
	env := NewEnvironment()
	loaded := 0
	env.SetLoader(func(name string) (string, error) {
		loaded++
		return "loaded " + name, nil
	})

	for _, source := range []string{
		`{% include "other.txt" %}`,
		`{% extends "base.txt" %}`,
		`{% import "m.txt" as m %}`,
		`{% from "m.txt" import thing %}`,
	} {
		if _, err := env.TemplateFromNamedString("main.txt", source); err == nil {
			t.Fatalf("%q compiled", source)
		}
	}
	if loaded != 0 {
		t.Fatalf("the loader was consulted %d times from template syntax", loaded)
	}
}

// bamlGatedTemplates lists every template in the inherited upstream conformance
// corpus that uses a statement this build does not have, with the keyword it
// trips on. They are not skipped: TestTemplates asserts each one fails to
// compile with that exact "unknown statement" error, which turns the inherited
// corpus into the widest available coverage of the gate.
//
// The map is exhaustive in both directions -- an input that is listed and
// compiles, or one that is not listed and trips the gate, fails the test.
//
// See internal/parser/features.go and PATCHES.md #2.
var bamlGatedTemplates = map[string]string{
	"block.txt":                         "block",
	"block_scope.txt":                   "block",
	"block_scope_extends.txt":           "extends",
	"block_scope_super.txt":             "extends",
	"block_super.html":                  "extends",
	"block_super.txt":                   "extends",
	"block_super_super.txt":             "extends",
	"do_macro_calling_macro.txt":        "from",
	"err_bad_basic_block.txt":           "extends",
	"err_bad_block.txt":                 "extends",
	"err_bad_super.txt":                 "extends",
	"err_block_in_macro.txt":            "block",
	"err_block_twice.txt":               "block",
	"err_extends_actually_not.txt":      "extends",
	"err_extends_twice.txt":             "extends",
	"err_in_include.txt":                "include",
	"err_no_super_block.txt":            "block",
	"err_self_extends.txt":              "extends",
	"err_self_include.txt":              "include",
	"err_toplevel_break.txt":            "break",
	"err_toplevel_continue.txt":         "continue",
	"extends.txt":                       "extends",
	"extends_set.txt":                   "extends",
	"import_all.txt":                    "import",
	"include.txt":                       "include",
	"include_choice_none.txt":           "include",
	"include_ignore_choice.txt":         "include",
	"include_ignore_missing.txt":        "include",
	"include_missing.txt":               "include",
	"loop_break_one_shot_iter.txt":      "break",
	"loop_break_one_shot_iter_peek.txt": "break",
	"loop_controls.txt":                 "continue",
	"macro_calling_macro.txt":           "from",
	"macro_extends.txt":                 "extends",
	"macro_import.txt":                  "from",
	"macro_import2.txt":                 "import",
	"macro_include.txt":                 "include",
	"self.txt":                          "block",
}

// bamlGatedRefTemplates is the same for the corpus's reference templates, which
// exist only to be {% include %}d or {% extends %}ed. Neither statement exists
// here, so these cannot be registered at all; the inputs that used them are in
// bamlGatedTemplates.
var bamlGatedRefTemplates = map[string]string{
	"bad_basic_block.txt":        "block",
	"layout_with_var.txt":        "block",
	"self-extends.txt":           "extends",
	"self-include.txt":           "include",
	"simple2_layout.txt":         "extends",
	"simple_layout.txt":          "block",
	"super_with_html.html":       "block",
	"var_referencing_layout.txt": "block",
	"var_setting_layout.txt":     "block",
}

// failIfUnlistedGateError is the other direction of the claim: a corpus entry
// that trips the gate without being listed would otherwise be absorbed by the
// deliberately fuzzy error comparison in TestTemplates and read as a pass.
func failIfUnlistedGateError(t *testing.T, name string, err error) {
	t.Helper()

	mjErr, ok := err.(*Error)
	if !ok || mjErr.Kind != ErrSyntax {
		return
	}
	const marker = "unknown statement "
	idx := strings.Index(mjErr.Message, marker)
	if idx < 0 {
		return
	}
	keyword := strings.Fields(mjErr.Message[idx+len(marker):])
	if len(keyword) == 0 {
		return
	}
	if _, gated := parser.GatedStatements()[keyword[0]]; !gated {
		return
	}
	if _, listed := bamlGatedTemplates[name]; !listed {
		t.Fatalf("%s trips the %q gate but is not listed in bamlGatedTemplates", name, keyword[0])
	}
}

// assertGatedTemplateSource requires that source fails to compile with the
// engine's "unknown statement <keyword>" error, and reports it against the
// corpus entry it came from.
func assertGatedTemplateSource(t *testing.T, name, keyword, source string) {
	t.Helper()

	env := NewEnvironment()
	if _, err := env.TemplateFromNamedString(name, source); err != nil {
		assertGateError(t, keyword, err)
		return
	}
	t.Fatalf("%s compiled, but it uses %q, which is not a statement in this build", name, keyword)
}
