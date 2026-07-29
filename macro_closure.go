package minijinja

import "github.com/invakid404/minijinja-go/v2/internal/parser"

// Whether a macro accepts the synthetic `caller` keyword is a LEXICAL question,
// and the engine answers it once, when it compiles the macro.
//
// `compile_macro_expression` computes the macro's closure — the set of variables
// its body reads without binding them — and sets the MACRO_CALLER flag if and
// only if `caller` is in it (compiler/codegen.rs:435-456):
//
//	let mut undeclared = crate::compiler::meta::find_macro_closure(macro_decl);
//	let caller_reference = undeclared.remove("caller");
//
// So it is free-variable analysis, not "does the token `caller` appear
// anywhere". The difference is observable, because the flag is what decides
// whether `f(caller=…)` is an accepted argument or `too_many_arguments`:
//
//   - `{% set caller = 5 %}` BINDS the name. A macro that only assigns it never
//     reads a free `caller`, and the engine rejects `f(caller=1)`.
//   - a nested `{% macro %}` (and a `{% call %}` block's implicit macro) is its
//     own scope, with `caller` already declared in it. A `caller()` inside the
//     inner macro is the INNER macro's binding and says nothing about the outer
//     one, which the engine also rejects `f(caller=1)` for.
//   - a default expression is visited AFTER the parameters are bound, so
//     `{% macro f(x=caller) %}` does read a free `caller` and the engine accepts
//     the keyword — the opposite direction from the two above.
//
// This file is the port of `compiler/meta.rs`'s AssignmentTracker for that one
// question. It is specialized to a single name: `is_assigned` is per-name in the
// reference and names never interact, so tracking only the name being asked
// about decides exactly what tracking every name would. The SCOPE discipline —
// which construct pushes a frame, which assigns before visiting, which does not
// visit a subexpression at all — is the reference's, statement for statement.
type closureTracker struct {
	// name is the variable whose freeness is being decided.
	name string
	// assigned is one frame per lexical scope; an entry is true once that frame
	// binds name. `is_assigned` looks through ALL live frames, so an inner scope
	// never shadows an outer binding away.
	assigned []bool
	// free records that name was read while no live frame bound it. It only
	// ever goes from false to true, which is what lets the walk stop early.
	free bool
}

// macroUsesCaller reports whether `caller` is a free variable of the macro, the
// engine's MACRO_CALLER flag.
func macroUsesCaller(macro *parser.Macro) bool {
	t := &closureTracker{name: "caller", assigned: []bool{false}}
	t.visitMacro(macro, false)
	return t.free
}

func (t *closureTracker) isAssigned() bool {
	for _, frame := range t.assigned {
		if frame {
			return true
		}
	}
	return false
}

func (t *closureTracker) assign(name string) {
	if name == t.name {
		t.assigned[len(t.assigned)-1] = true
	}
}

func (t *closureTracker) push() { t.assigned = append(t.assigned, false) }

func (t *closureTracker) pop() { t.assigned = t.assigned[:len(t.assigned)-1] }

// visitMacro is `tracker_visit_macro`. declareCaller is the reference's flag for
// a macro reached as a nested declaration or as a `{% call %}` block's body: the
// engine cannot know at compile time whether such a macro will really be called
// with a caller, so it errs on the side of declaring one.
func (t *closureTracker) visitMacro(m *parser.Macro, declareCaller bool) {
	if declareCaller {
		t.assign("caller")
	}
	for _, arg := range m.Args {
		t.assignTarget(arg)
	}
	for _, def := range m.Defaults {
		t.visitExpr(def)
	}
	for _, stmt := range m.Body {
		t.walk(stmt)
	}
}

// assignTarget is `track_assign`: an assignment target binds a plain name, and a
// tuple target binds each of its names. Anything else binds nothing.
func (t *closureTracker) assignTarget(expr parser.Expr) {
	switch e := expr.(type) {
	case *parser.Var:
		t.assign(e.ID)
	case *parser.List:
		for _, item := range e.Items {
			t.assignTarget(item)
		}
	}
}

// walk is `track_walk`.
//
// The multi-template statements are included as upstream writes them even
// though BAML's engine build omits the `multi_template` feature, so they cannot
// be reached from a Rust-side row.
func (t *closureTracker) walk(stmt parser.Stmt) {
	if t.free {
		return
	}
	switch st := stmt.(type) {
	case *parser.Template:
		t.assign("self")
		t.walkAll(st.Children)
	case *parser.EmitRaw:
	case *parser.EmitExpr:
		t.visitExpr(st.Expr)
	case *parser.ForLoop:
		t.push()
		t.assign("loop")
		t.visitExpr(st.Iter)
		t.assignTarget(st.Target)
		t.visitExpr(st.FilterExpr)
		t.walkAll(st.Body)
		t.pop()
		t.push()
		t.walkAll(st.ElseBody)
		t.pop()
	case *parser.IfCond:
		t.visitExpr(st.Expr)
		t.push()
		t.walkAll(st.TrueBody)
		t.pop()
		t.push()
		t.walkAll(st.FalseBody)
		t.pop()
	case *parser.WithBlock:
		t.push()
		for _, assignment := range st.Assignments {
			t.assignTarget(assignment.Target)
			t.visitExpr(assignment.Value)
		}
		t.walkAll(st.Body)
		t.pop()
	case *parser.Set:
		// The TARGET binds before the value is visited, so `{% set caller =
		// caller %}` reads a name that is already bound and is not a free use.
		t.assignTarget(st.Target)
		t.visitExpr(st.Expr)
	case *parser.AutoEscape:
		t.push()
		t.walkAll(st.Body)
		t.pop()
	case *parser.FilterBlock:
		// The reference does not visit the filter expression here.
		t.push()
		t.walkAll(st.Body)
		t.pop()
	case *parser.SetBlock:
		// Nor the capture's filter.
		t.assignTarget(st.Target)
		t.push()
		t.walkAll(st.Body)
		t.pop()
	case *parser.Block:
		t.push()
		t.assign("super")
		t.walkAll(st.Body)
		t.pop()
	case *parser.Extends, *parser.Include:
	case *parser.Import:
		t.assignTarget(st.Name)
	case *parser.FromImport:
		for _, name := range st.Names {
			if name.Alias != nil {
				t.assignTarget(name.Alias)
			} else {
				t.assignTarget(name.Name)
			}
		}
	case *parser.Macro:
		// The declaration binds the macro's own name in the ENCLOSING scope,
		// and its body is walked in a new one that already declares `caller`.
		t.assign(st.Name)
		t.push()
		t.visitMacro(st, true)
		t.pop()
	case *parser.CallBlock:
		t.visitExpr(st.Call.Expr)
		t.visitCallArgs(st.Call.Args)
		t.push()
		t.visitMacro(st.MacroDecl, true)
		t.pop()
	case *parser.Continue, *parser.Break:
	case *parser.Do:
		t.visitExpr(st.Call.Expr)
		t.visitCallArgs(st.Call.Args)
	}
}

func (t *closureTracker) walkAll(stmts []parser.Stmt) {
	for _, stmt := range stmts {
		t.walk(stmt)
	}
}

func (t *closureTracker) visitCallArgs(args []parser.CallArg) {
	for _, arg := range args {
		t.visitExpr(arg.Value)
	}
}

// visitExpr is `tracker_visit_expr` with nested tracking off, which is the mode
// `find_macro_closure` uses. A nil expression is the reference's Option::None.
func (t *closureTracker) visitExpr(expr parser.Expr) {
	if expr == nil || t.free {
		return
	}
	switch e := expr.(type) {
	case *parser.Var:
		if e.ID == t.name && !t.isAssigned() {
			t.free = true
		}
		// A name counts as assigned from its first lookup on, which is what
		// keeps a later binding from retroactively changing an earlier read.
		t.assign(e.ID)
	case *parser.Const:
	case *parser.UnaryOp:
		t.visitExpr(e.Expr)
	case *parser.BinOp:
		t.visitExpr(e.Left)
		t.visitExpr(e.Right)
	case *parser.IfExpr:
		t.visitExpr(e.TestExpr)
		t.visitExpr(e.TrueExpr)
		t.visitExpr(e.FalseExpr)
	case *parser.Filter:
		t.visitExpr(e.Expr)
		t.visitCallArgs(e.Args)
	case *parser.Test:
		t.visitExpr(e.Expr)
		t.visitCallArgs(e.Args)
	case *parser.GetAttr:
		t.visitExpr(e.Expr)
	case *parser.GetItem:
		t.visitExpr(e.Expr)
		t.visitExpr(e.SubscriptExpr)
	case *parser.Slice:
		// The reference visits the three bounds and NOT the sliced expression.
		t.visitExpr(e.Start)
		t.visitExpr(e.Stop)
		t.visitExpr(e.Step)
	case *parser.Call:
		t.visitExpr(e.Expr)
		t.visitCallArgs(e.Args)
	case *parser.List:
		for _, item := range e.Items {
			t.visitExpr(item)
		}
	case *parser.Map:
		for i := range e.Keys {
			t.visitExpr(e.Keys[i])
			if i < len(e.Values) {
				t.visitExpr(e.Values[i])
			}
		}
	}
}
