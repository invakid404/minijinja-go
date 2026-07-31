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
	"path/filepath"
	"sort"
	"strconv"
	"strings"
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
// Every profile below is *engine configuration only*: the same knobs the Rust
// Environment exposes, set identically on both sides, plus the generic
// unknown-method module BAML installs. BAML's own environment (get_env,
// regex_match/sum, prompt lowering, ctx/_/enum globals) is deliberately not one
// of them — it arrives as its own profile in a later slice, without a schema
// change.
//
// The whitespace profiles exist because trim_blocks/lstrip_blocks/
// keep_trailing_newline are not reachable from a template: they are set on the
// environment. BAML sets trim_blocks and lstrip_blocks in its own environment
// (jinja_helpers.rs:7-35), so the machinery those flags drive has to be
// compared under them, not only under the engine defaults.
type Profile string

const (
	// ProfileStock is stock engine defaults: no trimming, no lstrip, trailing
	// newline stripped.
	ProfileStock Profile = "stock"
	// ProfileTrimBlocks is set_trim_blocks(true).
	ProfileTrimBlocks Profile = "trim_blocks"
	// ProfileLstripBlocks is set_lstrip_blocks(true).
	ProfileLstripBlocks Profile = "lstrip_blocks"
	// ProfileTrimLstrip is both of the above — the combination BAML's own
	// environment uses.
	ProfileTrimLstrip Profile = "trim_lstrip"
	// ProfileKeepTrailingNewline is set_keep_trailing_newline(true).
	ProfileKeepTrailingNewline Profile = "keep_trailing_newline"
	// ProfilePycompat is stock defaults plus the Python-compatible
	// unknown-method callback — a generic, installable module (the fork's
	// pycompat package, minijinja-contrib's pycompat on the Rust side), which
	// is the one BAML installs (jinja_helpers.rs:34). It is deliberately NOT
	// BAML's environment: no regex_match, no sum, no none-formatter.
	ProfilePycompat Profile = "pycompat"
)

// KnownProfiles is the closed set of profiles both sides implement. A corpus
// naming anything else fails to load rather than being silently evaluated
// under the wrong environment.
var KnownProfiles = map[Profile]bool{
	ProfileStock:               true,
	ProfileTrimBlocks:          true,
	ProfileLstripBlocks:        true,
	ProfileTrimLstrip:          true,
	ProfileKeepTrailingNewline: true,
	ProfilePycompat:            true,
}

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
//
// The corpus is split across files by lane (seed, template, ...) rather than
// kept in one document: each file is recorded independently, so adding rows to
// one lane never invalidates another lane's recording.
type Corpus struct {
	SchemaVersion int   `json:"schema_version"`
	Rows          []Row `json:"rows"`

	// Name is the file's base name without extension ("seed", "template"). It
	// selects the recording that belongs to this corpus.
	Name string `json:"-"`
	// Path is where the corpus was loaded from.
	Path string `json:"-"`

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
	// KindOpaqueMap is a generic host object that is a map by REPRESENTATION
	// and has no enumerable pairs — `ObjectRepr::Map` with
	// `Enumerator::NonEnumerable` on the Rust side, `ObjectReprMap` without
	// `MapObject`/`MapGetter` on the fork's. It is the generic shape of BAML's
	// enum member and enum namespace objects, again with no BAML types
	// involved.
	//
	// A non-empty Canonical makes it a MEMBER: it answers the comparison hook
	// by that value. An empty one makes it a NAMESPACE, whose hook declines
	// everything, which is what lets a row reach the engine's map fallback.
	KindOpaqueMap ValueKind = "opaque_map"
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
	case KindNull, KindList, KindMap, KindCmpObject, KindOpaqueMap:
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
	c.Path = path
	c.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))

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
		if !KnownProfiles[row.Profile] {
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

// LoadCorpora loads every corpus file under dir, in stable filename order.
//
// Row ids are unique across the whole set, not merely within a file: the
// divergence ledger and PATCHES.md are keyed by id, so two lanes must never be
// able to claim the same one.
func LoadCorpora(dir string) ([]*Corpus, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no corpus files in %s", dir)
	}
	sort.Strings(paths)

	seen := make(map[string]string)
	out := make([]*Corpus, 0, len(paths))
	for _, path := range paths {
		c, err := LoadCorpus(path)
		if err != nil {
			return nil, err
		}
		for _, row := range c.Rows {
			if other, dup := seen[row.ID]; dup {
				return nil, fmt.Errorf("row id %q appears in both %s and %s", row.ID, other, path)
			}
			seen[row.ID] = path
		}
		out = append(out, c)
	}
	return out, nil
}

// RecordingPath is where a corpus's committed harness run lives. One recording
// per corpus file, so a change to one lane cannot stale another lane's.
func RecordingPath(root string, c *Corpus) string {
	return filepath.Join(root, RecordedDir, fmt.Sprintf("rust-%s-%s.json", EngineRevShort, c.Name))
}
