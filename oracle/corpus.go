// Package oracle is the differential oracle between this Go fork and BAML's
// pinned Rust minijinja engine (boundaryml/minijinja@8cfc770, the "value-cmp"
// branch).
//
// One corpus drives two engines. The Rust harness under oracle/harness emits an
// outcome per row; this package produces the same shape from the fork and
// compares them. Any mismatch must be declared in the divergence ledger with a
// class, so the differential stays green while every known red is on the record.
package oracle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
)

// SchemaVersion is the corpus/outcome schema version. It is checked on both
// sides so a schema change cannot silently compare two different fixtures.
const SchemaVersion = 1

// Form is how a row's source is turned into a template.
type Form string

const (
	// FormExpression wraps the source as `{{ expr }}` — the same shape BAML
	// uses to evaluate a constraint predicate (jinja_helpers.rs:67-94).
	FormExpression Form = "expression"
	// FormTemplate uses the source verbatim.
	FormTemplate Form = "template"
)

// Profile names the environment a row is evaluated in.
//
// Today the only profile is stock engine defaults. The BAML v0.223 profile
// (get_env, pycompat, regex_match/sum, prompt lowering) is a later slice; it
// arrives as a second profile on both sides without a schema change.
type Profile string

const ProfileStock Profile = "stock"

// Expect declares what the row is primarily asserting. It does not gate the
// comparison — outcomes are always compared in full — it selects boolean
// normalization and documents intent.
type Expect string

const (
	ExpectBytes   Expect = "bytes"
	ExpectBoolean Expect = "boolean"
	ExpectError   Expect = "error"
)

// Corpus is one differential fixture file.
type Corpus struct {
	SchemaVersion int   `json:"schema_version"`
	Rows          []Row `json:"rows"`

	// SHA256 of the exact bytes the corpus was loaded from. The Rust harness
	// records the same digest, which is what ties a recorded harness run to
	// the corpus it was produced from.
	SHA256 string `json:"-"`
}

// Row is a single differential case.
type Row struct {
	// ID is the stable, minimized identifier. Divergence ledger entries and
	// PATCHES.md deltas are keyed by it, so it must never be reused.
	ID string `json:"id"`
	// Surface is the divergence surface this row belongs to (arithmetic,
	// comparison, string, container, value_cmp, environment, control).
	Surface string `json:"surface,omitempty"`
	Form    Form   `json:"form"`
	Source  string `json:"source"`
	// Profile defaults to ProfileStock when empty.
	Profile Profile   `json:"profile,omitempty"`
	Inputs  []Binding `json:"inputs,omitempty"`
	// Expect defaults to ExpectBytes when empty.
	Expect Expect `json:"expect,omitempty"`
	Notes  string `json:"notes,omitempty"`
}

// TemplateSource returns the source actually handed to the engine.
func (r Row) TemplateSource() string {
	if r.Form == FormExpression {
		return "{{ " + r.Source + " }}"
	}
	return r.Source
}

// Binding is one named template input.
type Binding struct {
	Name  string     `json:"name"`
	Value TypedValue `json:"value"`
}

// MapEntry is one ordered map entry. Order is part of the fixture: BAML builds
// the engine with preserve_order.
type MapEntry struct {
	Key   string     `json:"key"`
	Value TypedValue `json:"value"`
}

// ValueKind tags a corpus input. Types are declared rather than inferred from
// JSON so int/float and map ordering survive into both runtimes unambiguously.
type ValueKind string

const (
	KindInt    ValueKind = "int"
	KindFloat  ValueKind = "float"
	KindBool   ValueKind = "bool"
	KindString ValueKind = "string"
	KindNull   ValueKind = "null"
	KindList   ValueKind = "list"
	KindMap    ValueKind = "map"
	// KindCmpObject is a generic host object that answers the engine's
	// comparison hook by a canonical value while displaying something else.
	// It is the generic shape of BAML's enum object (alias display,
	// canonical-value identity) with no BAML types involved.
	KindCmpObject ValueKind = "cmp_object"
)

// TypedValue is an explicitly typed corpus input.
type TypedValue struct {
	Kind ValueKind `json:"kind"`

	// Scalars. Int is kept as a json.Number so a 64-bit payload cannot be
	// silently rounded through float64 on the way in.
	Int    json.Number `json:"-"`
	Float  float64     `json:"-"`
	Bool   bool        `json:"-"`
	String string      `json:"-"`

	Items   []TypedValue `json:"items,omitempty"`
	Entries []MapEntry   `json:"entries,omitempty"`

	Canonical string `json:"canonical,omitempty"`
	Display   string `json:"display,omitempty"`
}

// rawTypedValue mirrors the wire form, where every scalar shares one "value" key.
type rawTypedValue struct {
	Kind      ValueKind       `json:"kind"`
	Value     json.RawMessage `json:"value,omitempty"`
	Items     []TypedValue    `json:"items,omitempty"`
	Entries   []MapEntry      `json:"entries,omitempty"`
	Canonical string          `json:"canonical,omitempty"`
	Display   string          `json:"display,omitempty"`
}

func (t *TypedValue) UnmarshalJSON(data []byte) error {
	var raw rawTypedValue
	// Scalars stay as RawMessage until their declared kind is known, so a
	// 64-bit integer payload is never routed through float64.
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*t = TypedValue{
		Kind:      raw.Kind,
		Items:     raw.Items,
		Entries:   raw.Entries,
		Canonical: raw.Canonical,
		Display:   raw.Display,
	}
	switch raw.Kind {
	case KindInt:
		var n json.Number
		if err := json.Unmarshal(raw.Value, &n); err != nil {
			return fmt.Errorf("int value: %w", err)
		}
		if _, err := n.Int64(); err != nil {
			return fmt.Errorf("int value %q is not an int64: %w", n, err)
		}
		t.Int = n
	case KindFloat:
		var n json.Number
		if err := json.Unmarshal(raw.Value, &n); err != nil {
			return fmt.Errorf("float value: %w", err)
		}
		f, err := n.Float64()
		if err != nil {
			return fmt.Errorf("float value %q is not a float64: %w", n, err)
		}
		t.Float = f
	case KindBool:
		if err := json.Unmarshal(raw.Value, &t.Bool); err != nil {
			return fmt.Errorf("bool value: %w", err)
		}
	case KindString:
		if err := json.Unmarshal(raw.Value, &t.String); err != nil {
			return fmt.Errorf("string value: %w", err)
		}
	case KindNull, KindList, KindMap, KindCmpObject:
		// no scalar payload
	default:
		return fmt.Errorf("unknown value kind %q", raw.Kind)
	}
	return nil
}

// Int64 returns the integer payload of a KindInt value.
func (t TypedValue) Int64() int64 {
	n, err := strconv.ParseInt(t.Int.String(), 10, 64)
	if err != nil {
		panic(fmt.Sprintf("corpus int %q is not an int64: %v", t.Int, err))
	}
	return n
}

// LoadCorpus reads and validates a corpus file.
func LoadCorpus(path string) (*Corpus, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Corpus
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if c.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("%s: schema_version %d, want %d", path, c.SchemaVersion, SchemaVersion)
	}
	sum := sha256.Sum256(raw)
	c.SHA256 = hex.EncodeToString(sum[:])

	seen := make(map[string]bool, len(c.Rows))
	for i := range c.Rows {
		row := &c.Rows[i]
		if row.ID == "" {
			return nil, fmt.Errorf("%s: row %d has no id", path, i)
		}
		if seen[row.ID] {
			return nil, fmt.Errorf("%s: duplicate row id %q", path, row.ID)
		}
		seen[row.ID] = true
		if row.Profile == "" {
			row.Profile = ProfileStock
		}
		if row.Profile != ProfileStock {
			return nil, fmt.Errorf("%s: row %q has unknown profile %q", path, row.ID, row.Profile)
		}
		if row.Expect == "" {
			row.Expect = ExpectBytes
		}
		switch row.Form {
		case FormExpression, FormTemplate:
		default:
			return nil, fmt.Errorf("%s: row %q has unknown form %q", path, row.ID, row.Form)
		}
	}
	return &c, nil
}
