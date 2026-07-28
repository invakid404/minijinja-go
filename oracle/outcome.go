package oracle

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Status is the coarse result of evaluating a row.
type Status string

const (
	StatusOK    Status = "ok"
	StatusError Status = "error"
	StatusPanic Status = "panic"
	// StatusUnsupported means the runtime could not model the row at all. It
	// is never a silent pass: a mismatch involving it is classified
	// harness-incomplete rather than as an engine divergence.
	StatusUnsupported Status = "unsupported"
)

// Outcome is what one engine did with one row. Both the Rust harness and the
// fork produce this shape.
type Outcome struct {
	Status Status `json:"status"`

	// Render is the exact rendered output. Compared byte for byte.
	Render string `json:"render,omitempty"`
	// Boolean is the normalized boolean, set only for Expect == boolean.
	Boolean *bool `json:"boolean,omitempty"`

	// Category is the canonical error category, shared vocabulary across both
	// implementations. This is what error comparison uses.
	Category string `json:"category,omitempty"`
	// Kind is the implementation's own name for the error, kept for diagnosis.
	Kind string `json:"kind,omitempty"`
	// Message is the implementation's error text. Deliberately NOT compared:
	// two engines are not expected to word an error identically, and pinning
	// message text would drown real divergences in noise.
	Message string `json:"message,omitempty"`
}

// Describe renders the outcome for a report or a ledger diff.
func (o Outcome) Describe() string {
	switch o.Status {
	case StatusOK:
		if o.Boolean != nil {
			return fmt.Sprintf("ok bool=%v render=%q", *o.Boolean, o.Render)
		}
		return fmt.Sprintf("ok render=%q", o.Render)
	case StatusError:
		return fmt.Sprintf("error category=%s kind=%s", o.Category, o.Kind)
	case StatusPanic:
		return fmt.Sprintf("panic %q", o.Message)
	case StatusUnsupported:
		return fmt.Sprintf("unsupported %q", o.Message)
	default:
		return fmt.Sprintf("unknown status %q", o.Status)
	}
}

// Signature is the part of an outcome the differential actually compares:
// status, exact bytes, normalized boolean and error category. Error message
// text is excluded on purpose.
func (o Outcome) Signature() string {
	switch o.Status {
	case StatusOK:
		b := "-"
		if o.Boolean != nil {
			b = fmt.Sprintf("%v", *o.Boolean)
		}
		return fmt.Sprintf("ok|%s|%s", b, o.Render)
	case StatusError:
		return "error|" + o.Category
	default:
		// A panic reduces to just "panic": aborting evaluation is the whole of
		// its semantic outcome, and the accompanying diagnostic is the host
		// language runtime's message rather than either engine's. That field is
		// deliberately not compared here — it is pinned from both sides by
		// panics_test.go, which also states the contract.
		return string(o.Status)
	}
}

// Equivalent reports whether two engines agreed on a row.
func (o Outcome) Equivalent(other Outcome) bool {
	return o.Signature() == other.Signature()
}

// NormalizeBoolean maps rendered output to a boolean the same way on both
// sides. Nil means the output was not a boolean at all, which is itself a
// comparable fact.
func NormalizeBoolean(rendered string) *bool {
	switch strings.TrimSpace(rendered) {
	case "true", "True":
		t := true
		return &t
	case "false", "False":
		f := false
		return &f
	}
	return nil
}

// HarnessOutput is the JSON document the Rust harness writes.
type HarnessOutput struct {
	SchemaVersion int             `json:"schema_version"`
	Provenance    Provenance      `json:"provenance"`
	Results       []HarnessResult `json:"results"`
	byID          map[string]Outcome
}

// HarnessResult is one row's outcome from the Rust harness.
type HarnessResult struct {
	ID      string  `json:"id"`
	Outcome Outcome `json:"outcome"`
}

// Provenance records exactly which engine produced a harness run, on what
// machine, against which corpus bytes.
type Provenance struct {
	EngineRepo     string   `json:"engine_repo"`
	EngineBranch   string   `json:"engine_branch"`
	EngineRev      string   `json:"engine_rev"`
	EngineFeatures []string `json:"engine_features"`
	HarnessVersion string   `json:"harness_version"`
	OS             string   `json:"os"`
	Arch           string   `json:"arch"`
	CorpusSHA256   string   `json:"corpus_sha256"`
}

// ParseHarnessOutput decodes and indexes a harness document.
func ParseHarnessOutput(raw []byte) (*HarnessOutput, error) {
	var out HarnessOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if out.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("harness schema_version %d, want %d", out.SchemaVersion, SchemaVersion)
	}
	out.byID = make(map[string]Outcome, len(out.Results))
	for _, r := range out.Results {
		if _, dup := out.byID[r.ID]; dup {
			return nil, fmt.Errorf("harness output has duplicate row id %q", r.ID)
		}
		out.byID[r.ID] = r.Outcome
	}
	return &out, nil
}

// Lookup returns the harness outcome for a row id.
func (h *HarnessOutput) Lookup(id string) (Outcome, bool) {
	o, ok := h.byID[id]
	return o, ok
}
