package minijinja

import (
	"testing"

	"github.com/invakid404/minijinja-go/v2/internal/parser"
	"github.com/invakid404/minijinja-go/v2/syntax"
)

// firstMacro compiles a template and returns the macro it declares first.
func firstMacro(t *testing.T, source string) *parser.Macro {
	t.Helper()
	tmpl, err := parser.Parse(source, "closure.txt", syntax.DefaultSyntax(), syntax.DefaultWhitespace())
	if err != nil {
		t.Fatalf("parsing %q: %v", source, err)
	}
	for _, stmt := range tmpl.Children {
		if macro, ok := stmt.(*parser.Macro); ok {
			return macro
		}
	}
	t.Fatalf("no macro in %q", source)
	return nil
}

// TestMacroUsesCallerIsFreeVariableAnalysis pins the decision itself rather than
// only what it renders. It is the engine's, not an approximation of it: the
// MACRO_CALLER flag is set iff `caller` is in the macro's closure, the set of
// variables its body reads without binding them (compiler/codegen.rs:435-456,
// compiler/meta.rs).
//
// Every case here is also a differential row (review/caller-lexical-*), so this
// test says WHY the fork answers what it answers while the corpus says that the
// engine answers the same.
func TestMacroUsesCallerIsFreeVariableAnalysis(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   bool
	}{
		// Plain reads.
		{"a bare call is a use", `{% macro f() %}{{ caller() }}{% endmacro %}`, true},
		{"so is an attribute of it", `{% macro f() %}{{ caller.x }}{% endmacro %}`, true},
		{"a sibling name is not", `{% macro f() %}{{ other() }}{% endmacro %}`, false},

		// Assignment targets BIND, they do not use.
		{"a set target binds", `{% macro f() %}{% set caller = 5 %}{% endmacro %}`, false},
		{"and binds before its value is read", `{% macro f() %}{% set caller = caller %}{% endmacro %}`, false},
		{"a tuple target binds each name", `{% macro f() %}{% set caller, x = [1,2] %}{% endmacro %}`, false},
		{"a capture target binds", `{% macro f() %}{% set caller %}y{% endset %}{% endmacro %}`, false},
		{"a loop target binds", `{% macro f() %}{% for caller in [1] %}{{ caller }}{% endfor %}{% endmacro %}`, false},
		{"a with target binds", `{% macro f() %}{% with caller = 1 %}{{ caller }}{% endwith %}{% endmacro %}`, false},
		{"a macro declaration binds its own name", `{% macro f() %}{% macro caller() %}n{% endmacro %}{{ caller() }}{% endmacro %}`, false},

		// ...and only for their own scope.
		{"a loop target does not escape the loop", `{% macro f() %}{% for caller in [1] %}{% endfor %}{{ caller }}{% endmacro %}`, true},
		{"nor does a with target", `{% macro f() %}{% with caller = 1 %}{% endwith %}{{ caller }}{% endmacro %}`, true},
		{"an if body is its own frame", `{% macro f() %}{% if true %}{% set caller = 1 %}{% endif %}{{ caller }}{% endmacro %}`, true},
		{"so is a filter block", `{% macro f() %}{% filter upper %}{% set caller = 1 %}{% endfilter %}{{ caller }}{% endmacro %}`, true},
		{"so is an autoescape block", `{% macro f() %}{% autoescape true %}{% set caller = 1 %}{% endautoescape %}{{ caller }}{% endmacro %}`, true},
		{"so is a for-else body", `{% macro f() %}{% for x in [] %}{% else %}{% set caller = 1 %}{% endfor %}{{ caller }}{% endmacro %}`, true},

		// A nested macro is a scope that already declares `caller`.
		{"a nested macro's use is its own", `{% macro outer() %}{% macro inner() %}{{ caller() }}{% endmacro %}O{% endmacro %}`, false},
		{"a nested macro's parameter is too", `{% macro outer() %}{% macro inner(caller) %}{{ caller }}{% endmacro %}{% endmacro %}`, false},
		{"the outer macro's own use still counts", `{% macro outer() %}{% macro inner() %}{{ caller() }}{% endmacro %}{{ caller() }}{% endmacro %}`, true},
		{"a call block's body is a nested macro", `{% macro f() %}{% call g() %}{{ caller() }}{% endcall %}{% endmacro %}`, false},
		{"but its call arguments are read here", `{% macro f() %}{% call g(caller) %}i{% endcall %}{% endmacro %}`, true},

		// Parameters bind before defaults are visited, so a default reads free.
		{"a declared parameter binds", `{% macro f(caller) %}{{ caller }}{% endmacro %}`, false},
		{"a default expression reads free", `{% macro f(x=caller) %}{{ x }}{% endmacro %}`, true},
		{"even after a parameter of the same name", `{% macro f(caller, x=caller) %}{{ x }}{% endmacro %}`, false},

		// Expression positions the reference does and does not visit.
		{"a loop's filter expression is visited", `{% macro f() %}{% for x in [1] if caller %}{{ x }}{% endfor %}{% endmacro %}`, true},
		{"a test argument is visited", `{% macro f() %}{{ 1 is divisibleby caller }}{% endmacro %}`, true},
		{"a map literal's key is visited", `{% macro f() %}{{ {caller: 1} }}{% endmacro %}`, true},
		{"a slice BOUND is visited", `{% macro f() %}{{ [1,2][caller:] }}{% endmacro %}`, true},
		{"a do call is visited", `{% macro f() %}{% do caller() %}{% endmacro %}`, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := macroUsesCaller(firstMacro(t, c.source)); got != c.want {
				t.Errorf("macroUsesCaller = %v, want %v\n  source: %s", got, c.want, c.source)
			}
		})
	}
}

// TestMacroClosureTracksOneNameExactly states the specialization the tracker
// makes. `is_assigned` is per-name in the reference and names never interact, so
// asking about one name decides exactly what building the whole closure would.
func TestMacroClosureTracksOneNameExactly(t *testing.T) {
	macro := firstMacro(t, `{% macro f() %}{% set other = 1 %}{{ caller() }}{% endmacro %}`)
	if !macroUsesCaller(macro) {
		t.Error("an unrelated binding changed the answer for `caller`")
	}

	tracker := &closureTracker{name: "other", assigned: []bool{false}}
	tracker.visitMacro(macro, false)
	if tracker.free {
		t.Error("`other` is bound by the set and must not be free")
	}
}
