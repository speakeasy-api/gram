// Package aitargets is the Shadow AI scan target catalog: the AI tools the
// device agent probes for and the on-device signatures it matches. The
// catalog lives in Postgres and is served to every enrolled agent inside the
// remote-configuration envelope on the plugin poll.
package aitargets

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Category classifies what kind of AI tool a target is.
type Category string

const (
	// CategoryHarness is an agentic coding tool or AI IDE.
	CategoryHarness Category = "harness"

	// CategoryLocalModel is a local model runtime.
	CategoryLocalModel Category = "local_model"
)

// KnownCategories lists every category the catalog, the scan-report ingest,
// and the device agent accept.
func KnownCategories() []Category {
	return []Category{CategoryHarness, CategoryLocalModel}
}

// SchemaVersion is the version of the ai_scan envelope delivered to agents.
const SchemaVersion = 1

// Signatures are the local footprints that identify one target on a device.
type Signatures struct {
	// BundleIDs are macOS CFBundleIdentifier values.
	BundleIDs []string `json:"bundle_ids"`

	// Binaries are bare command names resolved on the device PATH.
	Binaries []string `json:"binaries"`

	// ConfigDirs are directories whose existence marks the tool as
	// installed, resolved by the agent relative to the home folder unless
	// they start with "/".
	ConfigDirs []string `json:"config_dirs"`

	// ProcessNames are exact process names checked for the running signal.
	ProcessNames []string `json:"process_names"`
}

// VersionHint names the Info.plist key that carries a bundle-matched
// target's version; empty means CFBundleShortVersionString.
type VersionHint struct {
	// PlistKey is the Info.plist key to read.
	PlistKey string `json:"plist_key"`
}

// Target is one AI tool the catalog knows.
type Target struct {
	// ID is the stable catalog identifier scan reports key on.
	ID string `json:"id"`

	// DisplayName is shown in the dashboard.
	DisplayName string `json:"display_name"`

	// Category classifies the target.
	Category Category `json:"category"`

	// Signatures are the footprints the agent matches on the device.
	Signatures Signatures `json:"signatures"`

	// VersionHint overrides the Info.plist key used for version capture.
	VersionHint *VersionHint `json:"version_hint,omitempty"`

	// Enabled is false for targets kept for history but not served.
	Enabled bool `json:"enabled"`
}

// Clone returns a deep copy.
func (t Target) Clone() Target {
	out := t
	out.Signatures = Signatures{
		BundleIDs:    slices.Clone(t.Signatures.BundleIDs),
		Binaries:     slices.Clone(t.Signatures.Binaries),
		ConfigDirs:   slices.Clone(t.Signatures.ConfigDirs),
		ProcessNames: slices.Clone(t.Signatures.ProcessNames),
	}
	if t.VersionHint != nil {
		hint := *t.VersionHint
		out.VersionHint = &hint
	}
	return out
}

// Defaults is the code-owned set an empty catalog is seeded from. After the
// seed, Postgres is the source of truth.
func Defaults() []Target {
	return []Target{
		{
			ID:          "chatgpt-classic",
			DisplayName: "ChatGPT Classic",
			Category:    CategoryHarness,
			Signatures: Signatures{
				BundleIDs:    []string{"com.openai.chat"},
				Binaries:     []string{},
				ConfigDirs:   []string{},
				ProcessNames: []string{},
			},
			VersionHint: nil,
			Enabled:     true,
		},
		{
			ID:          "claude-code",
			DisplayName: "Claude Code",
			Category:    CategoryHarness,
			Signatures: Signatures{
				BundleIDs:    []string{},
				Binaries:     []string{"claude"},
				ConfigDirs:   []string{"~/.claude"},
				ProcessNames: []string{"claude"},
			},
			VersionHint: nil,
			Enabled:     true,
		},
		{
			ID:          "cursor",
			DisplayName: "Cursor",
			Category:    CategoryHarness,
			Signatures: Signatures{
				BundleIDs:    []string{"com.todesktop.230313mzl4w4u92"},
				Binaries:     []string{"cursor"},
				ConfigDirs:   []string{"~/.cursor"},
				ProcessNames: []string{"Cursor"},
			},
			VersionHint: nil,
			Enabled:     true,
		},
		{
			ID:          "codex",
			DisplayName: "Codex",
			Category:    CategoryHarness,
			Signatures: Signatures{
				BundleIDs:    []string{},
				Binaries:     []string{"codex"},
				ConfigDirs:   []string{"~/.codex"},
				ProcessNames: []string{"codex"},
			},
			VersionHint: nil,
			Enabled:     true,
		},
		{
			ID:          "gemini-cli",
			DisplayName: "Gemini CLI",
			Category:    CategoryHarness,
			Signatures: Signatures{
				BundleIDs:    []string{},
				Binaries:     []string{"gemini"},
				ConfigDirs:   []string{"~/.gemini"},
				ProcessNames: []string{"gemini"},
			},
			VersionHint: nil,
			Enabled:     true,
		},
		{
			ID:          "windsurf",
			DisplayName: "Windsurf",
			Category:    CategoryHarness,
			Signatures: Signatures{
				BundleIDs:    []string{"com.exafunction.windsurf"},
				Binaries:     []string{"windsurf"},
				ConfigDirs:   []string{"~/.codeium/windsurf"},
				ProcessNames: []string{"Windsurf"},
			},
			VersionHint: nil,
			Enabled:     true,
		},
		{
			ID:          "aider",
			DisplayName: "Aider",
			Category:    CategoryHarness,
			Signatures: Signatures{
				BundleIDs:    []string{},
				Binaries:     []string{"aider"},
				ConfigDirs:   []string{"~/.aider"},
				ProcessNames: []string{"aider"},
			},
			VersionHint: nil,
			Enabled:     true,
		},
		{
			ID:          "opencode",
			DisplayName: "opencode",
			Category:    CategoryHarness,
			Signatures: Signatures{
				BundleIDs:    []string{},
				Binaries:     []string{"opencode"},
				ConfigDirs:   []string{"~/.config/opencode"},
				ProcessNames: []string{"opencode"},
			},
			VersionHint: nil,
			Enabled:     true,
		},
		{
			ID:          "openclaw",
			DisplayName: "OpenClaw",
			Category:    CategoryHarness,
			Signatures: Signatures{
				BundleIDs:    []string{},
				Binaries:     []string{"openclaw"},
				ConfigDirs:   []string{"~/.openclaw"},
				ProcessNames: []string{"openclaw"},
			},
			VersionHint: nil,
			Enabled:     true,
		},
		{
			ID:          "ollama",
			DisplayName: "Ollama",
			Category:    CategoryLocalModel,
			Signatures: Signatures{
				BundleIDs:    []string{"com.electron.ollama"},
				Binaries:     []string{"ollama"},
				ConfigDirs:   []string{"~/.ollama"},
				ProcessNames: []string{"ollama"},
			},
			VersionHint: nil,
			Enabled:     true,
		},
		{
			ID:          "lmstudio",
			DisplayName: "LM Studio",
			Category:    CategoryLocalModel,
			Signatures: Signatures{
				BundleIDs:    []string{"ai.elementlabs.lmstudio"},
				Binaries:     []string{"lms"},
				ConfigDirs:   []string{"~/.lmstudio"},
				ProcessNames: []string{"LM Studio"},
			},
			VersionHint: nil,
			Enabled:     true,
		},
	}
}

// Snapshot is an immutable view of the served catalog at one list version.
type Snapshot struct {
	// ListVersion is the catalog revision, echoed by agents on receipts.
	ListVersion int32

	// ETag fingerprints the schema version, ListVersion, and the served
	// targets.
	ETag string

	targets  []Target
	byID     map[string]Target
	envelope map[string]any
}

// NewSnapshot builds a snapshot over a copy of targets, sorted by id.
func NewSnapshot(listVersion int32, targets []Target) *Snapshot {
	owned := make([]Target, 0, len(targets))
	byID := make(map[string]Target, len(targets))
	for _, target := range targets {
		cloned := target.Clone()
		owned = append(owned, cloned)
		byID[cloned.ID] = cloned
	}
	slices.SortFunc(owned, func(a, b Target) int {
		return strings.Compare(a.ID, b.ID)
	})
	snapshot := &Snapshot{
		ListVersion: listVersion,
		ETag:        fingerprint(listVersion, owned),
		targets:     owned,
		byID:        byID,
		envelope:    nil,
	}
	snapshot.envelope = encodeEnvelope(snapshot.Envelope())
	return snapshot
}

// Targets returns the served targets, ordered by id, as a copy.
func (s *Snapshot) Targets() []Target {
	out := make([]Target, 0, len(s.targets))
	for _, target := range s.targets {
		out = append(out, target.Clone())
	}
	return out
}

// ByID resolves a served target; unknown ids resolve to ok=false.
func (s *Snapshot) ByID(id string) (Target, bool) {
	target, ok := s.byID[id]
	if !ok {
		return Target{ID: "", DisplayName: "", Category: "", Signatures: Signatures{BundleIDs: nil, Binaries: nil, ConfigDirs: nil, ProcessNames: nil}, VersionHint: nil, Enabled: false}, false
	}
	return target.Clone(), true
}

// Envelope is the ai_scan object served to agents. Its JSON shape is the
// wire contract the device agent decodes; only ever add to it.
type Envelope struct {
	// SchemaVersion is SchemaVersion at the time of serving.
	SchemaVersion int `json:"schema_version"`

	// ListVersion is the catalog revision the targets were read at.
	ListVersion int32 `json:"list_version"`

	// ETag fingerprints the list.
	ETag string `json:"etag"`

	// Targets are the enabled targets, ordered by id.
	Targets []Target `json:"targets"`
}

// Envelope returns the wire form of the snapshot.
func (s *Snapshot) Envelope() Envelope {
	return Envelope{
		SchemaVersion: SchemaVersion,
		ListVersion:   s.ListVersion,
		ETag:          s.ETag,
		Targets:       s.Targets(),
	}
}

// EnvelopeValue returns the envelope as the generic JSON value the plugin
// poll embeds in the configuration document, encoded once per snapshot.
// Callers must not mutate it.
func (s *Snapshot) EnvelopeValue() map[string]any {
	return s.envelope
}

func encodeEnvelope(envelope Envelope) map[string]any {
	data, err := json.Marshal(envelope)
	if err != nil {
		return map[string]any{}
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		return map[string]any{}
	}
	return value
}

func fingerprint(listVersion int32, sorted []Target) string {
	hash := sha256.New()
	_, _ = fmt.Fprintf(hash, "schema_version=%d\nlist_version=%d\n", SchemaVersion, listVersion)
	data, err := json.Marshal(sorted)
	if err != nil {
		_, _ = fmt.Fprintf(hash, "marshal error: %v", err)
	}
	_, _ = hash.Write(data)
	return hex.EncodeToString(hash.Sum(nil))
}
