package parser

import (
	"fmt"
	"strings"
	"testing"
)

// gatedParserInputs lists the entries of the inherited upstream parser corpus
// that use a statement this build does not have, with the keyword they trip on.
// They are asserted rather than skipped, so the corpus keeps proving the gate
// instead of merely tolerating it.
//
// See features.go and PATCHES.md #2.
var gatedParserInputs = map[string]string{
	"block.txt":                "block",
	"err_open_block.txt":       "block",
	"err_wrong_block_name.txt": "block",
	"extends.txt":              "extends",
	"imports.txt":              "from",
	"include.txt":              "include",
}

func assertGatedParse(t *testing.T, name, keyword string, result Result) {
	t.Helper()

	if result.Err == nil {
		t.Fatalf("%s parsed, but %q is not a statement in this build", name, keyword)
	}
	want := fmt.Sprintf("unknown statement %s", keyword)
	if !strings.Contains(result.Err.Detail, want) {
		t.Fatalf("%s: expected %q, got %q", name, want, result.Err.Detail)
	}
}

// assertNotAnUnlistedGate keeps gatedParserInputs exhaustive: a corpus entry
// that starts tripping the gate must be listed deliberately, not discovered as
// a confusing snapshot diff.
func assertNotAnUnlistedGate(t *testing.T, name string, result Result) {
	t.Helper()

	if result.Err == nil {
		return
	}
	const marker = "unknown statement "
	idx := strings.Index(result.Err.Detail, marker)
	if idx < 0 {
		return
	}
	fields := strings.Fields(result.Err.Detail[idx+len(marker):])
	if len(fields) == 0 {
		return
	}
	if _, gated := gatedStatements[fields[0]]; gated {
		t.Fatalf("%s trips the %q gate but is not listed in gatedParserInputs", name, fields[0])
	}
}

// TestGatedStatementsRejected is the unit-level statement of the gate: every
// keyword in the map is rejected by the parser with the engine's exact message,
// through the same path a typo takes.
func TestGatedStatementsRejected(t *testing.T) {
	sources := map[string]string{
		"block":    `{% block b %}x{% endblock %}`,
		"extends":  `{% extends "b.txt" %}`,
		"include":  `{% include "b.txt" %}`,
		"import":   `{% import "b.txt" as b %}`,
		"from":     `{% from "b.txt" import x %}`,
		"break":    `{% for x in xs %}{% break %}{% endfor %}`,
		"continue": `{% for x in xs %}{% continue %}{% endfor %}`,
	}

	for keyword := range gatedStatements {
		source, ok := sources[keyword]
		if !ok {
			t.Errorf("gated statement %q has no test case", keyword)
			continue
		}
		t.Run(keyword, func(t *testing.T) {
			assertGatedParse(t, keyword, keyword, ParseDefault(source, "gated.txt"))
		})
	}
}

// TestUngatedStatementsStillParse guards the blast radius: the gate must reject
// only what BAML's feature set omits.
func TestUngatedStatementsStillParse(t *testing.T) {
	for _, source := range []string{
		`{% if x %}a{% elif y %}b{% else %}c{% endif %}`,
		`{% for x in xs %}{{ x }}{% else %}none{% endfor %}`,
		`{% set x = 1 %}{% set y %}body{% endset %}`,
		`{% with x = 1 %}{{ x }}{% endwith %}`,
		`{% filter upper %}x{% endfilter %}`,
		`{% autoescape true %}x{% endautoescape %}`,
		`{% macro m(a) %}{{ a }}{% endmacro %}{% call m(1) %}body{% endcall %}`,
		`{% raw %}{% include "x" %}{% endraw %}`,
		`{# {% include "x" %} #}`,
		`{% do m() %}`,
	} {
		t.Run(source, func(t *testing.T) {
			if result := ParseDefault(source, "ok.txt"); result.Err != nil {
				t.Fatalf("%s failed to parse: %v", source, result.Err)
			}
		})
	}
}
