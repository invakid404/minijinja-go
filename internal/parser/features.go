package parser

// Engine feature set.
//
// BAML builds minijinja with `default-features = false` and an explicit feature
// list (BoundaryML/baml@85247f45 `engine/Cargo.toml:99-115`): macros, builtins,
// debug, preserve_order, adjacent_loop_items, unicode, json, custom_syntax and
// deserialization. `multi_template`, `loader`, `loop_controls` and `fuel` are
// deliberately not in it.
//
// In Rust that is not a runtime setting: the statement keywords those features
// carry are `#[cfg]`-ed out of the parser entirely, so the dispatch in
// `compiler/parser.rs:776-816` falls through to `unknown statement <name>` — a
// syntax error, before any template can be resolved or loaded.
//
// This fork reproduces that. A template BAML's engine cannot compile does not
// compile here either, and no loader can be reached from template syntax by
// accident. The map is the single place that decides it: re-enabling a feature
// is one deletion here plus its statement arm in parseStmt.
//
// Proven by the tmpl/negative-* corpus rows.
var gatedStatements = map[string]string{
	// multi_template: template inheritance, inclusion and module imports.
	"block":   "multi_template",
	"extends": "multi_template",
	"include": "multi_template",
	"import":  "multi_template",
	"from":    "multi_template",
	// loop_controls: early exit from a loop body.
	"break":    "loop_controls",
	"continue": "loop_controls",
}

// GatedStatements returns the statement keywords this build rejects because the
// engine feature that carries them is not enabled, keyed to that feature.
//
// Exported for tests that assert the gate rather than infer it.
func GatedStatements() map[string]string {
	out := make(map[string]string, len(gatedStatements))
	for name, feature := range gatedStatements {
		out[name] = feature
	}
	return out
}
