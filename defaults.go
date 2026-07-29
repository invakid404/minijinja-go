package minijinja

import (
	"fmt"
	"sort"
	"strings"

	"github.com/invakid404/minijinja-go/v2/filters"
	"github.com/invakid404/minijinja-go/v2/tests"
	"github.com/invakid404/minijinja-go/v2/value"
)

// The default registry is exactly the builtin set BAML's engine build enables.
//
// BAML compiles minijinja with `default-features = false` plus `macros,
// builtins, debug, preserve_order, adjacent_loop_items, unicode, json,
// unstable_machinery, custom_syntax, deserialization, serde`
// (BoundaryML/baml@85247f45 engine/Cargo.toml:99-115), so the engine's
// registry is `defaults.rs:64-236` under that feature set. Diffing it against
// the Go port's original defaults left exactly five names the port had and the
// engine does not, and they are deliberately **not** registered here:
//
//	filter    urlencode    gated behind the engine's `urlencode` feature, which BAML does not enable
//	test      containing   not in minijinja 2.16.0 at all
//	function  cycler       idem
//	function  joiner       idem
//	function  lipsum       idem
//
// Answering them would be the dangerous direction: a template BAML rejects
// outright would silently render here. Leaving them unregistered produces the
// engine's own unknown-filter/test/function error instead.
// filters.FilterUrlencode and tests.TestContaining remain exported for callers
// who opt in explicitly; nothing reaches them by default. The three functions
// had no exported form and are gone. Corpus: `err/go-only-*`.
func registerDefaultFilters(env *Environment) {
	// String filters
	env.AddFilter("upper", filters.FilterUpper)
	env.AddFilter("lower", filters.FilterLower)
	env.AddFilter("capitalize", filters.FilterCapitalize)
	env.AddFilter("title", filters.FilterTitle)
	env.AddFilter("trim", filters.FilterTrim)
	env.AddFilter("replace", filters.FilterReplace)
	env.AddFilter("format", filters.FilterFormat)
	env.AddFilter("default", filters.FilterDefault)
	env.AddFilter("d", filters.FilterDefault) // alias
	env.AddFilter("safe", filters.FilterSafe)
	env.AddFilter("escape", filters.FilterEscape)
	env.AddFilter("e", filters.FilterEscape) // alias
	env.AddFilter("string", filters.FilterString)
	env.AddFilter("bool", filters.FilterBool)
	env.AddFilter("split", filters.FilterSplit)
	env.AddFilter("lines", filters.FilterLines)

	// List/sequence filters
	env.AddFilter("length", filters.FilterLength)
	env.AddFilter("count", filters.FilterLength) // alias
	env.AddFilter("first", filters.FilterFirst)
	env.AddFilter("last", filters.FilterLast)
	env.AddFilter("reverse", filters.FilterReverse)
	env.AddFilter("sort", filters.FilterSort)
	env.AddFilter("join", filters.FilterJoin)
	env.AddFilter("list", filters.FilterList)
	env.AddFilter("unique", filters.FilterUnique)
	env.AddFilter("min", filters.FilterMin)
	env.AddFilter("max", filters.FilterMax)
	env.AddFilter("sum", filters.FilterSum)
	env.AddFilter("batch", filters.FilterBatch)
	env.AddFilter("slice", filters.FilterSlice)
	env.AddFilter("map", filters.FilterMap)
	env.AddFilter("select", filters.FilterSelect)
	env.AddFilter("reject", filters.FilterReject)
	env.AddFilter("selectattr", filters.FilterSelectAttr)
	env.AddFilter("rejectattr", filters.FilterRejectAttr)
	env.AddFilter("groupby", filters.FilterGroupBy)
	env.AddFilter("chain", filters.FilterChain)
	env.AddFilter("zip", filters.FilterZip)

	// Numeric filters
	env.AddFilter("abs", filters.FilterAbs)
	env.AddFilter("int", filters.FilterInt)
	env.AddFilter("float", filters.FilterFloat)
	env.AddFilter("round", filters.FilterRound)

	// Dict filters
	env.AddFilter("items", filters.FilterItems)
	env.AddFilter("dictsort", filters.FilterDictSort)

	// Other filters
	env.AddFilter("attr", filters.FilterAttr)
	env.AddFilter("indent", filters.FilterIndent)
	env.AddFilter("pprint", filters.FilterPprint)

	// JSON filters
	env.AddFilter("tojson", filters.FilterTojson)
}

func registerDefaultTests(env *Environment) {
	env.AddTest("defined", tests.TestDefined)
	env.AddTest("undefined", tests.TestUndefined)
	env.AddTest("none", tests.TestNone)
	env.AddTest("true", tests.TestTrue)
	env.AddTest("false", tests.TestFalse)
	env.AddTest("odd", tests.TestOdd)
	env.AddTest("even", tests.TestEven)
	env.AddTest("divisibleby", tests.TestDivisibleBy)
	env.AddTest("eq", tests.TestEq)
	env.AddTest("equalto", tests.TestEq)
	env.AddTest("==", tests.TestEq)
	env.AddTest("ne", tests.TestNe)
	env.AddTest("!=", tests.TestNe)
	env.AddTest("lt", tests.TestLt)
	env.AddTest("lessthan", tests.TestLt)
	env.AddTest("<", tests.TestLt)
	env.AddTest("le", tests.TestLe)
	env.AddTest("<=", tests.TestLe)
	env.AddTest("gt", tests.TestGt)
	env.AddTest("greaterthan", tests.TestGt)
	env.AddTest(">", tests.TestGt)
	env.AddTest("ge", tests.TestGe)
	env.AddTest(">=", tests.TestGe)
	env.AddTest("in", tests.TestIn)
	env.AddTest("string", tests.TestString)
	env.AddTest("number", tests.TestNumber)
	env.AddTest("integer", tests.TestInteger)
	env.AddTest("int", tests.TestInteger) // alias
	env.AddTest("float", tests.TestFloat)
	env.AddTest("boolean", tests.TestBoolean)
	env.AddTest("sequence", tests.TestSequence)
	env.AddTest("mapping", tests.TestMapping)
	env.AddTest("iterable", tests.TestIterable)
	env.AddTest("startingwith", tests.TestStartingWith)
	env.AddTest("endingwith", tests.TestEndingWith)
	env.AddTest("safe", tests.TestSafe)
	env.AddTest("escaped", tests.TestSafe) // alias
	env.AddTest("sameas", tests.TestSameAs)
	env.AddTest("lower", tests.TestLower)
	env.AddTest("upper", tests.TestUpper)
	env.AddTest("filter", tests.TestFilter)
	env.AddTest("test", tests.TestTest)
}

// registerDefaultFunctions registers the engine's four globals
// (defaults.rs:211-236). `cycler`, `joiner` and `lipsum` are Go-port additions
// and are withdrawn; see the note on registerDefaultFilters.
func registerDefaultFunctions(env *Environment) {
	env.AddFunction("range", fnRange)
	env.AddFunction("dict", fnDict)
	env.AddFunction("namespace", fnNamespace)
	env.AddFunction("debug", fnDebug)
}

// --- Functions ---

// prettyRepr is Rust's alternate debug format, `{:#?}`: a container is printed
// one element per line, indented four spaces per level, with a trailing comma
// on every element. A scalar prints the same as its compact debug form.
func prettyRepr(val value.Value, depth int) string {
	pad := strings.Repeat("    ", depth)
	inner := pad + "    "

	switch val.Kind() {
	case value.KindSeq, value.KindIterable:
		items := val.Iter()
		if len(items) == 0 {
			return "[]"
		}
		var b strings.Builder
		b.WriteString("[\n")
		for _, item := range items {
			b.WriteString(inner)
			b.WriteString(prettyRepr(item, depth+1))
			b.WriteString(",\n")
		}
		b.WriteString(pad)
		b.WriteString("]")
		return b.String()
	case value.KindMap:
		keys, _ := val.MapKeys()
		if len(keys) == 0 {
			return "{}"
		}
		ordered, hasOrder := val.AsOrderedMap()
		var b strings.Builder
		b.WriteString("{\n")
		for _, key := range keys {
			b.WriteString(inner)
			if hasOrder {
				b.WriteString(ordered.KeyRepr(key))
			} else {
				b.WriteString(value.FromString(key).Repr())
			}
			b.WriteString(": ")
			b.WriteString(prettyRepr(val.GetItem(value.FromString(key)), depth+1))
			b.WriteString(",\n")
		}
		b.WriteString(pad)
		b.WriteString("}")
		return b.String()
	default:
		return val.Repr()
	}
}

// mappingEntries copies a value the engine would accept as a mapping. That is
// wider than "a plain map": the loop object and an imported module are mappings
// too (`ObjectRepr::Map`), which is what `dict(loop, extra=2)` relies on.
// sortedNames is the deterministic order for a mapping with no order of its
// own.
func sortedNames(m map[string]value.Value) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func mappingEntries(val value.Value, present bool) (*value.OrderedMap, error) {
	rv := value.NewOrderedMap(0)
	if !present {
		return rv, nil
	}
	// An ordered mapping is copied entry by entry so each key keeps how it was
	// SPELLED: `dict({8: 8})` renders `{8: 8}` and not `{"8": 8}`, because the
	// engine collects the source's key VALUES (`obj.try_iter_pairs().collect()`,
	// functions.rs:397-400) rather than their string forms.
	if src, ok := val.AsOrderedMap(); ok {
		for _, name := range src.Keys() {
			rv.CopyEntryFrom(src, name)
		}
		return rv, nil
	}
	// Through MapKeys so the copy keeps the source's own order. It also
	// accepts the mapping-shaped objects the engine does — the loop object and
	// an imported module — which is what `dict(loop, extra=2)` relies on.
	if keys, ok := val.MapKeys(); ok {
		for _, name := range keys {
			rv.Set(name, val.GetAttr(name))
		}
		return rv, nil
	}
	if m, ok := val.AsMap(); ok {
		for _, name := range sortedNames(m) {
			rv.Set(name, m[name])
		}
		return rv, nil
	}
	return nil, fmt.Errorf("not a mapping")
}

func fnRange(_ *State, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	// `range(lower: isize, upper: Option<isize>, step: Option<isize>)`
	// (functions.rs:326-360). There is no Kwargs parameter, so a keyword
	// argument arrives as a trailing value and lands in the next integer slot,
	// where a map is not an integer.
	a := filters.NewArgs(args, kwargs)
	lower, err := a.Int("isize")
	if err != nil {
		return value.Undefined(), err
	}
	upper, hasUpper, err := a.OptInt("isize")
	if err != nil {
		return value.Undefined(), err
	}
	step, hasStep, err := a.OptInt("isize")
	if err != nil {
		return value.Undefined(), err
	}
	if err := a.Done(); err != nil {
		return value.Undefined(), err
	}

	start, stop := int64(0), lower
	if hasUpper {
		start, stop = lower, upper
	}
	if !hasStep {
		step = 1
	}
	if step == 0 {
		return value.Undefined(), NewError(ErrInvalidOperation, "cannot create range with step of 0")
	}

	// The cardinality is computed in UNSIGNED arithmetic and the loop then runs
	// a checked number of times. Computing `stop - start` in int64 overflows
	// near the boundaries — `range(MaxInt64-1, MaxInt64, 2)` produced a length
	// of one but a loop whose first increment wrapped to MinInt64 and never
	// reached the odd stop again, so it allocated forever. Two's-complement
	// subtraction gives the true distance whenever the endpoints are ordered,
	// so the count is exact at both ends of the range.
	count := rangeCount(start, stop, step)
	if count > 100000 {
		return value.Undefined(), NewError(ErrInvalidOperation, "range has too many elements")
	}

	result := make([]value.Value, 0, count)
	i := start
	for n := uint64(0); n < count; n++ {
		result = append(result, value.FromInt(i))
		if n+1 < count {
			// Only advance while another element is coming: the step past the
			// last one could overflow, and nothing would read it.
			i += step
		}
	}
	return value.FromIterator(value.NewIterator("range", result)), nil
}

// rangeCount is how many values `range(start, stop, step)` yields, computed
// without signed overflow.
func rangeCount(start, stop, step int64) uint64 {
	var span, magnitude uint64
	if step > 0 {
		if stop <= start {
			return 0
		}
		span = uint64(stop) - uint64(start)
		magnitude = uint64(step)
	} else {
		if stop >= start {
			return 0
		}
		span = uint64(start) - uint64(stop)
		// -step overflows for MinInt64; the magnitude is exact in unsigned.
		magnitude = -uint64(step)
	}
	count := span / magnitude
	if span%magnitude != 0 {
		count++
	}
	return count
}

func fnDict(_ *State, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	// `dict(value: Option<Value>, update_with: Kwargs)` (functions.rs:394-414):
	// the keyword arguments are taken first, then the optional positional
	// value, which must be a mapping if it is given at all.
	a := filters.NewArgs(args, kwargs).KwargsAll()
	base, hasBase, err := a.OptValue()
	if err != nil {
		return value.Undefined(), err
	}
	if err := a.Done(); err != nil {
		return value.Undefined(), err
	}

	result, err := mappingEntries(base, hasBase)
	if err != nil {
		return value.Undefined(), NewError(ErrInvalidOperation, "invalid operation")
	}
	// CopyEntryFrom, not Set: an entry splatted out of `**{8: 8}` reached the
	// kwargs map with its key SPELLING intact, and copying only the string form
	// would render it back as `{"8": 8}` where the engine renders `{8: 8}`.
	// The spelling travels with the entry all the way to the result.
	for _, k := range kwargs.Keys() {
		result.CopyEntryFrom(kwargs, k)
	}
	return value.FromOrderedMap(result), nil
}

func fnNamespace(_ *State, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	// `namespace(defaults: Option<Value>)` (functions.rs:452-475). There is no
	// Kwargs parameter: `namespace(count=0)` works because the keyword
	// arguments arrive as a map and land in `defaults`. A second positional
	// argument is therefore one too many.
	a := filters.NewArgs(args, kwargs)
	defaults, hasDefaults, err := a.OptValue()
	if err != nil {
		return value.Undefined(), err
	}
	if err := a.Done(); err != nil {
		return value.Undefined(), err
	}

	data, err := mappingEntries(defaults, hasDefaults)
	if err != nil {
		return value.Undefined(), NewError(ErrInvalidOperation,
			fmt.Sprintf("expected object or keyword arguments, got %s", defaults.Kind()))
	}
	ns := &namespaceValue{data: data.Map()}
	return value.FromObject(ns), nil
}

// namespaceValue is a mutable namespace object
type namespaceValue struct {
	data map[string]value.Value
}

func (n *namespaceValue) GetAttr(name string) value.Value {
	if v, ok := n.data[name]; ok {
		return v
	}
	return value.Undefined()
}

func (n *namespaceValue) SetAttr(name string, val value.Value) {
	n.data[name] = val
}

func (n *namespaceValue) String() string {
	keys := make([]string, 0, len(n.data))
	for k := range n.data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%q: %s", k, n.data[k].Repr()))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func (n *namespaceValue) Map() map[string]value.Value {
	return n.data
}

func fnDebug(state *State, args []value.Value, kwargs *value.OrderedMap) (value.Value, error) {
	// `debug(state, args: Rest<Value>)` (functions.rs:430-450): keyword
	// arguments are part of the debugged list, as one trailing map, so
	// `debug(foo=1)` is not the same call as `debug()`.
	args = filters.NewArgs(args, kwargs).Rest()

	// One argument is pretty-debugged on its own; several are pretty-debugged
	// as the argument SLICE (functions.rs:431-439). The port joined the
	// compact forms with ", ", so `debug(1, 2)` was `1, 2` where the engine
	// prints a multi-line list.
	if len(args) == 1 {
		return value.FromString(prettyRepr(args[0], 0)), nil
	}
	if len(args) > 1 {
		return value.FromString(prettyRepr(value.FromSlice(args), 0)), nil
	}

	// Otherwise debug the current state
	var parts []string
	parts = append(parts, fmt.Sprintf("State {"))
	parts = append(parts, fmt.Sprintf("  name: %q,", state.name))
	parts = append(parts, "  current variables: {")

	// Collect variables from scopes
	for i := len(state.scopes) - 1; i >= 0; i-- {
		keys := make([]string, 0, len(state.scopes[i]))
		for k := range state.scopes[i] {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("    %s: %s,", k, state.scopes[i][k].Repr()))
		}
	}
	if !state.rootContext.IsUndefined() {
		if obj, ok := state.rootContext.AsObject(); ok {
			if m, ok := obj.(value.MapObject); ok {
				keys := m.Keys()
				sort.Strings(keys)
				for _, k := range keys {
					parts = append(parts, fmt.Sprintf("    %s: %s,", k, state.rootContext.GetAttr(k).Repr()))
				}
			}
		}
	}
	parts = append(parts, "  }")
	parts = append(parts, "}")

	return value.FromString(strings.Join(parts, "\n")), nil
}
