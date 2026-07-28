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

func fnRange(_ *State, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
	var start, stop, step int64 = 0, 0, 1

	// `range(lower: isize, upper: Option<isize>, step: Option<isize>)`
	// (functions.rs:326-360): a non-integer argument is a conversion error,
	// not a silently empty range.
	rangeArg := func(v value.Value) (int64, error) {
		n, ok := v.AsInt()
		if !ok || !v.IsActualInt() {
			return 0, NewError(ErrInvalidOperation, fmt.Sprintf("cannot convert %s to isize", v.Kind()))
		}
		return n, nil
	}
	var err error
	switch len(args) {
	case 0:
		return value.Undefined(), NewError(ErrMissingArgument, "missing argument")
	case 1:
		if stop, err = rangeArg(args[0]); err != nil {
			return value.Undefined(), err
		}
	case 2:
		if start, err = rangeArg(args[0]); err != nil {
			return value.Undefined(), err
		}
		if stop, err = rangeArg(args[1]); err != nil {
			return value.Undefined(), err
		}
	case 3:
		if start, err = rangeArg(args[0]); err != nil {
			return value.Undefined(), err
		}
		if stop, err = rangeArg(args[1]); err != nil {
			return value.Undefined(), err
		}
		if step, err = rangeArg(args[2]); err != nil {
			return value.Undefined(), err
		}
	default:
		return value.Undefined(), NewError(ErrTooManyArguments, "received too many arguments")
	}

	if step == 0 {
		return value.Undefined(), NewError(ErrInvalidOperation, "cannot create range with step of 0")
	}

	length := int64(0)
	if step > 0 {
		if stop > start {
			length = (stop - start + step - 1) / step
		}
	} else {
		if stop < start {
			negStep := -step
			length = (start - stop + negStep - 1) / negStep
		}
	}
	if length > 100000 {
		return value.Undefined(), NewError(ErrInvalidOperation, "range has too many elements")
	}

	var result []value.Value
	if step > 0 {
		for i := start; i < stop; i += step {
			result = append(result, value.FromInt(i))
		}
	} else {
		for i := start; i > stop; i += step {
			result = append(result, value.FromInt(i))
		}
	}
	return value.FromIterator(value.NewIterator("range", result)), nil
}

func fnDict(_ *State, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
	result := make(map[string]value.Value)

	// First, copy from first positional argument if it's a map
	if len(args) > 0 {
		if m, ok := args[0].AsMap(); ok {
			for k, v := range m {
				result[k] = v
			}
		} else {
			// Try to iterate as items
			items := args[0].Iter()
			if items != nil {
				for _, item := range items {
					if pair, ok := item.AsSlice(); ok && len(pair) == 2 {
						if k, ok := pair[0].AsString(); ok {
							result[k] = pair[1]
						}
					}
				}
			}
		}
	}

	// Then apply kwargs (overwriting)
	for k, v := range kwargs {
		result[k] = v
	}
	return value.FromMap(result), nil
}

func fnNamespace(_ *State, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
	ns := &namespaceValue{
		data: make(map[string]value.Value),
	}

	// If first argument is a map, copy from it
	if len(args) > 0 {
		if m, ok := args[0].AsMap(); ok {
			for k, v := range m {
				ns.data[k] = v
			}
		} else if !args[0].IsUndefined() && !args[0].IsNone() {
			return value.Undefined(), NewError(ErrInvalidOperation, "namespace expects a mapping")
		}
	}

	// Apply kwargs
	for k, v := range kwargs {
		ns.data[k] = v
	}
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

func fnDebug(state *State, args []value.Value, _ map[string]value.Value) (value.Value, error) {
	// If arguments provided, debug those values
	if len(args) > 0 {
		var parts []string
		for _, arg := range args {
			parts = append(parts, arg.Repr())
		}
		return value.FromString(strings.Join(parts, ", ")), nil
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
