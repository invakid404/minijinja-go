package oracle

import (
	"encoding/json"
	"fmt"
	"os"
)

// Class labels which layer a divergence belongs to.
//
// The scope's three-way rule is: engine agrees but the whole BAML stack differs
// implies the BAML profile/adapter layer; the fork differs from both implies the
// fork's engine; all three differ means the harness is incomplete or the fixture
// is wrong. Slice 1 only has two of the three runtimes, so it can name the
// engine layer and it can name a harness gap; ClassProfile and ClassHost exist
// so a row can be reclassified without a schema change once the CFFI lane lands.
type Class string

const (
	// ClassEngine: this fork's engine behaves differently from the pinned Rust
	// engine on the same inputs. Fixing it is a fork change.
	ClassEngine Class = "engine"
	// ClassProfile: the divergence belongs to the BAML environment/value
	// adapter layer, not the engine. Not yet observable in slice 1.
	ClassProfile Class = "profile"
	// ClassHost: the divergence belongs to host integration. Not yet
	// observable in slice 1.
	ClassHost Class = "host"
	// ClassHarnessIncomplete: the harness could not model the row faithfully,
	// so the difference is not evidence about either engine.
	ClassHarnessIncomplete Class = "harness-incomplete"
)

func (c Class) valid() bool {
	switch c {
	case ClassEngine, ClassProfile, ClassHost, ClassHarnessIncomplete:
		return true
	}
	return false
}

// Ledger is the permanent record of known divergences.
//
// Every mismatch on the corpus must appear here. An undeclared mismatch fails
// the differential, and so does a declared mismatch that has stopped
// diverging or has changed shape — a fixed divergence must be removed
// deliberately, and a moved one must be re-examined rather than absorbed.
type Ledger struct {
	SchemaVersion int           `json:"schema_version"`
	Entries       []LedgerEntry `json:"entries"`
	byID          map[string]*LedgerEntry
}

// LedgerEntry declares one known divergence.
type LedgerEntry struct {
	// ID is the corpus row id.
	ID    string `json:"id"`
	Class Class  `json:"class"`
	// Summary states the difference in one line.
	Summary string `json:"summary"`
	// Rust and Go are the outcome signatures observed when the entry was
	// recorded. If either side moves, the entry no longer matches and the
	// differential fails.
	Rust string `json:"rust_signature"`
	Go   string `json:"go_signature"`
	// Slice names where this is expected to be resolved, per the scope's slice
	// plan. Empty means not yet scheduled.
	Slice string `json:"slice,omitempty"`
}

// LoadLedger reads and validates the divergence ledger.
func LoadLedger(path string) (*Ledger, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var l Ledger
	if err := json.Unmarshal(raw, &l); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if l.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("%s: schema_version %d, want %d", path, l.SchemaVersion, SchemaVersion)
	}
	l.byID = make(map[string]*LedgerEntry, len(l.Entries))
	for i := range l.Entries {
		e := &l.Entries[i]
		if e.ID == "" {
			return nil, fmt.Errorf("%s: entry %d has no id", path, i)
		}
		if _, dup := l.byID[e.ID]; dup {
			return nil, fmt.Errorf("%s: duplicate entry id %q", path, e.ID)
		}
		if !e.Class.valid() {
			return nil, fmt.Errorf("%s: entry %q has unknown class %q", path, e.ID, e.Class)
		}
		if e.Summary == "" {
			return nil, fmt.Errorf("%s: entry %q has no summary", path, e.ID)
		}
		l.byID[e.ID] = e
	}
	return &l, nil
}

// Lookup returns the ledger entry for a row id.
func (l *Ledger) Lookup(id string) (*LedgerEntry, bool) {
	e, ok := l.byID[id]
	return e, ok
}
