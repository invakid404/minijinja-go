package value

import "sort"

// OrderedMap is a mapping that remembers the order its keys were inserted in.
//
// BAML builds its Rust engine with the `preserve_order` feature, where the
// engine's map value is an insertion-ordered IndexMap. Rendering, iteration,
// `items()`, JSON serialization and `for` loops therefore all observe insertion
// order, and prompt bytes depend on it. A Go `map[string]Value` cannot carry
// that information, so an engine value backed by one has to pick an order;
// [FromMap] sorts, which is deterministic but not what the Rust engine does.
//
// OrderedMap is the representation for a mapping whose order is known: map
// literals in a template, and mappings a host builds deliberately. Values built
// from it compare, iterate, render and serialize in insertion order.
//
// A key that is set twice keeps its original position and takes the newer
// value, which is what IndexMap::insert does.
//
//	m := NewOrderedMap(2)
//	m.Set("b", FromInt(1))
//	m.Set("a", FromInt(2))
//	v := FromOrderedMap(m)  // renders as {"b": 1, "a": 2}
//
// An OrderedMap must not be mutated after the Value wrapping it is handed to
// the engine; the engine treats values as immutable.
type OrderedMap struct {
	keys []string
	vals map[string]Value

	// keyReprs remembers how a key was originally spelled, for keys that were
	// not strings. The Rust engine keys its maps by Value, so `{1: 'a'}` renders
	// its key as 1 while a map with the string key "1" renders it as "1". This
	// fork keys by string, so the spelling is kept alongside rather than
	// recovered from the key. Only non-string keys appear here.
	keyReprs map[string]string
}

// NewOrderedMap creates an empty OrderedMap with room for capacity entries.
func NewOrderedMap(capacity int) *OrderedMap {
	if capacity < 0 {
		capacity = 0
	}
	return &OrderedMap{
		keys: make([]string, 0, capacity),
		vals: make(map[string]Value, capacity),
	}
}

// OrderedMapFromPairs builds an OrderedMap from key/value pairs in order.
func OrderedMapFromPairs(keys []string, values []Value) *OrderedMap {
	n := len(keys)
	if len(values) < n {
		n = len(values)
	}
	m := NewOrderedMap(n)
	for i := 0; i < n; i++ {
		m.Set(keys[i], values[i])
	}
	return m
}

// Set inserts or replaces a key. An existing key keeps its position.
func (m *OrderedMap) Set(key string, val Value) {
	if m.vals == nil {
		m.vals = make(map[string]Value, 1)
	}
	if _, exists := m.vals[key]; !exists {
		m.keys = append(m.keys, key)
	}
	m.vals[key] = val
}

// SetKeyValue inserts or replaces an entry whose key is an arbitrary value.
//
// The key is indexed by its string form — this fork's mappings are keyed by
// string — but its original spelling is remembered, so a map literal written
// with a numeric key renders with a numeric key. Looking such an entry up by a
// non-string key is not supported; see PATCHES.md.
func (m *OrderedMap) SetKeyValue(key Value, val Value) {
	name, isString := key.AsString()
	if !isString {
		name = key.String()
	}
	m.Set(name, val)
	if !isString {
		if m.keyReprs == nil {
			m.keyReprs = make(map[string]string, 1)
		}
		m.keyReprs[name] = key.Repr()
	}
}

// KeyRepr returns the debug spelling of a key: the key value's own
// representation when it was not a string, and the quoted string otherwise.
func (m *OrderedMap) KeyRepr(key string) string {
	if m != nil {
		if repr, ok := m.keyReprs[key]; ok {
			return repr
		}
	}
	return FromString(key).Repr()
}

// CopyEntryFrom copies one entry, keeping how its key was spelled.
//
// A key splatted out of `{8: 8}` renders as `8` and not as `"8"`, so a copy
// that only carried the string form would change the bytes.
func (m *OrderedMap) CopyEntryFrom(src *OrderedMap, key string) {
	if src == nil {
		return
	}
	val, ok := src.Get(key)
	if !ok {
		return
	}
	m.Set(key, val)
	if repr, isSpelled := src.keyReprs[key]; isSpelled {
		if m.keyReprs == nil {
			m.keyReprs = make(map[string]string, 1)
		}
		m.keyReprs[key] = repr
	}
}

// Delete removes a key, keeping the order of the rest.
func (m *OrderedMap) Delete(key string) {
	if m == nil {
		return
	}
	if _, exists := m.vals[key]; !exists {
		return
	}
	delete(m.vals, key)
	delete(m.keyReprs, key)
	for i, k := range m.keys {
		if k == key {
			m.keys = append(m.keys[:i], m.keys[i+1:]...)
			return
		}
	}
}

// Get returns the value for a key.
func (m *OrderedMap) Get(key string) (Value, bool) {
	if m == nil {
		return Undefined(), false
	}
	v, ok := m.vals[key]
	return v, ok
}

// Len returns the number of entries.
func (m *OrderedMap) Len() int {
	if m == nil {
		return 0
	}
	return len(m.keys)
}

// Keys returns the keys in insertion order.
//
// The returned slice is a copy: callers must not be able to reorder a value the
// engine considers immutable.
func (m *OrderedMap) Keys() []string {
	if m == nil {
		return nil
	}
	out := make([]string, len(m.keys))
	copy(out, m.keys)
	return out
}

// Map returns the entries as an unordered Go map. It satisfies MapGetter, so
// AsMap works on values backed by an OrderedMap.
//
// The order is lost, which is exactly why the engine's own map handling uses
// [Value.MapKeys] rather than ranging over this.
func (m *OrderedMap) Map() map[string]Value {
	if m == nil {
		return nil
	}
	out := make(map[string]Value, len(m.vals))
	for k, v := range m.vals {
		out[k] = v
	}
	return out
}

// Clone returns a copy that can be mutated independently.
func (m *OrderedMap) Clone() *OrderedMap {
	if m == nil {
		return nil
	}
	out := NewOrderedMap(len(m.keys))
	for _, k := range m.keys {
		out.Set(k, m.vals[k])
	}
	for k, repr := range m.keyReprs {
		if out.keyReprs == nil {
			out.keyReprs = make(map[string]string, len(m.keyReprs))
		}
		out.keyReprs[k] = repr
	}
	return out
}

// FromOrderedMap creates a Value from an insertion-ordered mapping.
func FromOrderedMap(m *OrderedMap) Value {
	if m == nil {
		m = NewOrderedMap(0)
	}
	return Value{data: m}
}

// AsOrderedMap returns the insertion-ordered mapping backing this value, if it
// has one. A value built from a Go map does not: it has no order to report.
func (v Value) AsOrderedMap() (*OrderedMap, bool) {
	m, ok := v.data.(*OrderedMap)
	return m, ok
}

// MapKeys returns a mapping's keys in the order the engine iterates them:
// insertion order for an ordered mapping, sorted for one built from a Go map,
// whose iteration order would otherwise be random.
//
// Every part of the engine that enumerates a mapping goes through this, so
// display, iteration, `items`, `list`, `first` and JSON all agree. The result is
// a fresh slice: callers such as `dictsort` sort it in place.
func (v Value) MapKeys() ([]string, bool) {
	switch d := v.data.(type) {
	case *OrderedMap:
		return d.Keys(), true
	case map[string]Value:
		return sortedKeys(d), true
	}
	// An object may report its keys directly, or only expose the mapping. Both
	// are checked, in that order: a type that implements Object and MapGetter
	// but not MapObject still has enumerable keys.
	if mo, ok := v.data.(MapObject); ok {
		if repr := GetObjectRepr(mo); repr == ObjectReprMap || repr == ObjectReprPlain {
			return mo.Keys(), true
		}
	}
	if mg, ok := v.data.(MapGetter); ok {
		return sortedKeys(mg.Map()), true
	}
	return nil, false
}

// sortedKeys is the deterministic order for a mapping that has no order of its
// own. It is not the Rust engine's order — that is insertion order, which a Go
// map cannot carry — but it is stable across runs, which random iteration is
// not.
func sortedKeys(m map[string]Value) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
