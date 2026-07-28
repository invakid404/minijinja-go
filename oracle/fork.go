package oracle

import (
	"errors"
	"fmt"
	"strings"

	minijinja "github.com/invakid404/minijinja-go/v2"
	"github.com/invakid404/minijinja-go/v2/value"
)

// cmpObject is the Go counterpart of the Rust harness's CmpObject: a generic
// host object with a canonical comparison identity and a different display
// string. It is the generic shape of BAML's enum object, with no BAML types
// involved.
//
// This fork's object protocol only offers object-to-object comparison
// (value.ObjectWithCmp, dispatched at value/ops.go:420-425). BoundaryML's
// engine additionally asks an object to compare itself against an arbitrary
// Value before ordinary equality/ordering. The two sides implement the same
// canonical-value semantics; whatever the differential reports about them is a
// statement about comparison dispatch, not about the fixture.
type cmpObject struct {
	canonical string
	display   string
}

var (
	_ value.Object           = (*cmpObject)(nil)
	_ value.ObjectWithString = (*cmpObject)(nil)
	_ value.ObjectWithCmp    = (*cmpObject)(nil)
	_ fmt.Stringer           = (*cmpObject)(nil)
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

// BuildValue converts a corpus input into a fork value.
//
// Maps go through value.FromMap, which is the idiomatic way a consumer passes a
// mapping to this engine. That the port then sorts the keys is exactly the
// behaviour the differential is here to observe, so it is not worked around.
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
		m := make(map[string]value.Value, len(tv.Entries))
		for _, e := range tv.Entries {
			v, err := BuildValue(e.Value)
			if err != nil {
				return value.Undefined(), fmt.Errorf("map key %q: %w", e.Key, err)
			}
			m[e.Key] = v
		}
		return value.FromMap(m), nil
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
// The fork has no counterpart for several Rust kinds (unknown_method,
// non_primitive, non_key, cannot_unpack, bad_serialization, write_failure).
// That asymmetry is real and shows up as a category divergence rather than
// being smoothed over here.
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

// RunFork evaluates a row against this fork and reports the outcome in the same
// shape the Rust harness uses.
//
// A panic is an outcome, not a crashed test run: the parked evaluator work
// recorded panics and hangs in this area, and losing them would understate the
// divergence surface.
func RunFork(row Row) (out Outcome) {
	defer func() {
		if r := recover(); r != nil {
			out = Outcome{Status: StatusPanic, Message: fmt.Sprint(r)}
		}
	}()

	if row.Profile != ProfileStock {
		return Outcome{Status: StatusUnsupported, Message: "unknown engine profile"}
	}

	env := minijinja.NewEnvironment()
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
