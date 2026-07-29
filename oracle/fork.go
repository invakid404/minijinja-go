package oracle

import (
	"errors"
	"fmt"
	"strings"
	"time"

	minijinja "github.com/invakid404/minijinja-go/v2"
	"github.com/invakid404/minijinja-go/v2/pycompat"
	"github.com/invakid404/minijinja-go/v2/syntax"
	"github.com/invakid404/minijinja-go/v2/value"
)

// cmpObject is the Go counterpart of the Rust harness's CmpObject: a generic
// host object with a canonical comparison identity and a different display
// string. It is the generic shape of BAML's enum object, with no BAML types
// involved.
//
// Both sides implement the same canonical-value semantics through the same
// generic hook — value.ObjectWithValueCmp here, Object::value_cmp in Rust — so
// whatever the differential reports about these rows is a statement about the
// engine's comparison dispatch, not about the fixture.
type cmpObject struct {
	canonical string
	display   string
}

var (
	_ value.Object             = (*cmpObject)(nil)
	_ value.ObjectWithString   = (*cmpObject)(nil)
	_ value.ObjectWithCmp      = (*cmpObject)(nil)
	_ value.ObjectWithValueCmp = (*cmpObject)(nil)
	_ fmt.Stringer             = (*cmpObject)(nil)
)

func (o *cmpObject) GetAttr(string) value.Value { return value.Undefined() }

// String is what actually drives display: Value.String falls through to
// fmt.Sprintf("%v", obj) for objects (value/value.go:947-948), so an object
// author on this fork implements fmt.Stringer.
func (o *cmpObject) String() string { return o.display }

// ObjectString satisfies value.ObjectWithString, the interface the fork
// documents for custom display. The engine never consults it — the port's own
// test says so (value/object_test.go:628-638) — so it is implemented here for
// completeness and does not affect the fixture.
func (o *cmpObject) ObjectString() string { return o.display }

func (o *cmpObject) ObjectCmp(other value.Object) (int, bool) {
	oo, ok := other.(*cmpObject)
	if !ok {
		return 0, false
	}
	return strings.Compare(o.canonical, oo.canonical), true
}

// ValueCmp is the generic comparison hook, implemented exactly as the Rust
// harness implements Object::value_cmp: compare to a string by canonical value,
// delegate to the object-to-object hook for objects, and decline anything else.
func (o *cmpObject) ValueCmp(other value.Value) (int, bool) {
	if s, ok := other.AsString(); ok {
		return strings.Compare(o.canonical, s), true
	}
	if obj, ok := other.AsObject(); ok {
		return o.ObjectCmp(obj)
	}
	return 0, false
}

// BuildValue converts a corpus input into a fork value.
//
// A corpus map declares its entry order, and that order is part of the fixture
// because BAML builds its engine with preserve_order. It is therefore built with
// the fork's order-preserving mapping, which is the same thing the Rust side
// does with Value::from_iter. A host that has no order to declare would use
// value.FromMap instead and get a deterministic, sorted one.
func BuildValue(tv TypedValue) (value.Value, error) {
	switch tv.Kind {
	case KindInt:
		return value.FromInt(tv.Int64()), nil
	case KindFloat:
		return value.FromFloat(tv.Float), nil
	case KindBool:
		return value.FromBool(tv.Bool), nil
	case KindString:
		return value.FromString(tv.String), nil
	case KindNull:
		return value.None(), nil
	case KindList:
		items := make([]value.Value, 0, len(tv.Items))
		for i, item := range tv.Items {
			v, err := BuildValue(item)
			if err != nil {
				return value.Undefined(), fmt.Errorf("list item %d: %w", i, err)
			}
			items = append(items, v)
		}
		return value.FromSlice(items), nil
	case KindMap:
		m := value.NewOrderedMap(len(tv.Entries))
		for _, e := range tv.Entries {
			v, err := BuildValue(e.Value)
			if err != nil {
				return value.Undefined(), fmt.Errorf("map key %q: %w", e.Key, err)
			}
			m.Set(e.Key, v)
		}
		return value.FromOrderedMap(m), nil
	case KindCmpObject:
		return value.FromObject(&cmpObject{canonical: tv.Canonical, display: tv.Display}), nil
	default:
		return value.Undefined(), fmt.Errorf("unknown value kind %q", tv.Kind)
	}
}

// errorCategory maps this fork's error kinds onto the canonical vocabulary the
// Rust harness also emits. Categories, not message text, are what the
// differential compares.
//
// The fork still has no counterpart for several Rust kinds (non_primitive,
// non_key, bad_serialization, write_failure). That asymmetry is real and shows
// up as a category divergence rather than being smoothed over here.
// cannot_unpack and unknown_method were two of them until the template and
// builtins slices added them (PATCHES.md #6, #8).
func errorCategory(kind minijinja.ErrorKind) string {
	switch kind {
	case minijinja.ErrSyntax:
		return "syntax"
	case minijinja.ErrUndefinedVar:
		return "undefined"
	case minijinja.ErrUnknownFilter:
		return "unknown_filter"
	case minijinja.ErrUnknownTest:
		return "unknown_test"
	case minijinja.ErrUnknownFunction:
		return "unknown_function"
	case minijinja.ErrInvalidOperation:
		return "invalid_operation"
	case minijinja.ErrTemplateNotFound:
		return "template_not_found"
	case minijinja.ErrBadEscape:
		return "bad_escape"
	case minijinja.ErrUnknownBlock:
		return "unknown_block"
	case minijinja.ErrMissingArgument:
		return "missing_argument"
	case minijinja.ErrTooManyArguments:
		return "too_many_arguments"
	case minijinja.ErrBadInclude:
		return "bad_include"
	case minijinja.ErrOutOfFuel:
		return "out_of_fuel"
	case minijinja.ErrEvalBlock:
		return "eval_block"
	case minijinja.ErrCannotUnpack:
		return "cannot_unpack"
	case minijinja.ErrUnknownMethod:
		return "unknown_method"
	default:
		return "other"
	}
}

// kindName is the fork's own name for an error kind, recorded for diagnosis.
func kindName(kind minijinja.ErrorKind) string {
	return kind.String()
}

func forkError(err error) Outcome {
	var mjErr *minijinja.Error
	if errors.As(err, &mjErr) {
		return Outcome{
			Status:   StatusError,
			Category: errorCategory(mjErr.Kind),
			Kind:     kindName(mjErr.Kind),
			Message:  mjErr.Error(),
		}
	}
	// Template compilation errors come back as *parser.Error, an unexported
	// internal type, so an external consumer of the fork cannot classify them
	// at all. Rather than string-matching the message into a category — which
	// would paper over the gap and could mask a genuinely different error
	// later — the outcome says so, and the row is carried as a declared
	// divergence until the fork exposes a classifiable error.
	return Outcome{
		Status:   StatusError,
		Category: "unclassifiable",
		Kind:     fmt.Sprintf("%T (not *minijinja.Error)", err),
		Message:  err.Error(),
	}
}

// environmentFor builds the environment a row's profile names. Every profile is
// engine configuration only, set exactly as the Rust harness sets it, plus the
// generic unknown-method module for the pycompat profile; no filter, function
// or global is registered on either side.
func environmentFor(profile Profile) (*minijinja.Environment, bool) {
	ws := syntax.DefaultWhitespace()
	pycompatMethods := false
	switch profile {
	case ProfileStock:
	case ProfileTrimBlocks:
		ws.TrimBlocks = true
	case ProfileLstripBlocks:
		ws.LstripBlocks = true
	case ProfileTrimLstrip:
		ws.TrimBlocks = true
		ws.LstripBlocks = true
	case ProfileKeepTrailingNewline:
		ws.KeepTrailingNewline = true
	case ProfilePycompat:
		// The generic capability the fork exposes, driven by the installable
		// module BAML installs on its own environment. Nothing else of BAML's
		// environment is set up here.
		pycompatMethods = true
	default:
		return nil, false
	}
	env := minijinja.NewEnvironment()
	env.SetWhitespace(ws)
	if pycompatMethods {
		env.SetUnknownMethodCallback(pycompat.UnknownMethodCallback)
	}
	return env, true
}

// RunFork evaluates a row against this fork and reports the outcome in the same
// shape the Rust harness uses.
//
// A panic is an outcome, not a crashed test run: the parked evaluator work
// recorded panics and hangs in this area, and losing them would understate the
// divergence surface.
// RowTimeout bounds one row's evaluation on the Go side, matching the Rust
// harness's bound (harness/src/main.rs ROW_TIMEOUT). A row that does not
// terminate is an outcome, not a hung run.
//
// It separates "does not terminate" from "is slow", so it sits far above the
// slowest real row rather than close to it: every row in the corpus but one
// finishes in microseconds, and the margin is what makes the differential
// reliable on a loaded shared runner. Five seconds was not enough margin — CI
// recorded a timeout on `review/pprint-key-mixed`, a pretty-print of a
// three-entry map, and failed the merge gate on something that is not a
// divergence. The one row that always spends the whole budget
// (`review/pycompat-count-empty-needle`) loops forever on the Rust side, so any
// finite value still catches it, and a timeout is compared as `timeout` with
// the number excluded — see [Outcome.Signature].
const RowTimeout = 30 * time.Second

// RunFork evaluates a row against this fork, bounded in time.
func RunFork(row Row) Outcome {
	done := make(chan Outcome, 1)
	go func() { done <- runForkRow(row) }()
	select {
	case out := <-done:
		return out
	case <-time.After(RowTimeout):
		// The goroutine is left behind; the process exits when the run is
		// done, and a row that hangs is exactly what this reports.
		return Outcome{Status: StatusTimeout, Seconds: int(RowTimeout / time.Second)}
	}
}

func runForkRow(row Row) (out Outcome) {
	defer func() {
		if r := recover(); r != nil {
			out = Outcome{Status: StatusPanic, Message: fmt.Sprint(r)}
		}
	}()

	env, ok := environmentFor(row.Profile)
	if !ok {
		return Outcome{Status: StatusUnsupported, Message: fmt.Sprintf("unknown engine profile %q", row.Profile)}
	}

	// The `.txt` name keeps the default auto-escape callback on "none" on both
	// sides, so escaping never silently colours a byte comparison.
	tmpl, err := env.TemplateFromNamedString("corpus.txt", row.TemplateSource())
	if err != nil {
		return forkError(err)
	}

	ctx := make(map[string]value.Value, len(row.Inputs))
	for _, b := range row.Inputs {
		v, err := BuildValue(b.Value)
		if err != nil {
			return Outcome{Status: StatusUnsupported, Message: fmt.Sprintf("input %q: %v", b.Name, err)}
		}
		ctx[b.Name] = v
	}

	rendered, err := tmpl.Render(value.FromMap(ctx))
	if err != nil {
		return forkError(err)
	}

	out = Outcome{Status: StatusOK, Render: rendered}
	if row.Expect == ExpectBoolean {
		out.Boolean = NormalizeBoolean(rendered)
	}
	return out
}
