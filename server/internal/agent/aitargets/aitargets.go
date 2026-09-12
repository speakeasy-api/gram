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

	"github.com/speakeasy-api/gram/server/internal/aivendors"
)

// Category classifies what kind of AI tool a target is.
type Category string

const (
	// CategoryHarness is an agentic coding tool or AI IDE.
	CategoryHarness Category = "harness"

	// CategoryAssistant is a general-purpose AI assistant or agent. Like a
	// harness it speaks MCP to Gram, so a decision about one is enforceable;
	// unlike a harness it is not a coding tool.
	CategoryAssistant Category = "assistant"

	// CategoryLocalModel is an open model run locally. The wire value stays
	// local_model: it is echoed by deployed agent binaries and stored on every
	// detection row, so the rename is a label, not a contract change.
	CategoryLocalModel Category = "local_model"
)

// KnownCategories lists every category the catalog, the scan-report ingest,
// and the device agent accept.
func KnownCategories() []Category {
	return []Category{CategoryHarness, CategoryAssistant, CategoryLocalModel}
}

// CallsGateway reports whether a target of this category ever reaches Gram's
// MCP gateway, and so whether naming a caller for it is meaningful. It is the
// one place that distinction is written down: a category that never connects
// cannot carry matchers and cannot be the subject of an access decision.
func CallsGateway(category Category) bool {
	return category == CategoryHarness || category == CategoryAssistant
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

// GatewayClient links a scan target to the MCP gateway callers that are the
// same tool. A device signature and an OAuth caller share no natural join
// key — one is a laptop footprint, the other a registered client — so the
// link is declared here rather than derived.
//
// The three lists are not interchangeable. CIMDVendorKeys and OAuthClientIDs
// name credentials the server itself verified, so a decision may be enforced
// on them. ClientInfoNames names what the client called itself, which is
// attribution only; see MatchGatewayCaller.
type GatewayClient struct {
	// CIMDVendorKeys match the VendorKey of the CIMD catalog entry that
	// admitted the caller's client_id. Vendor-grained: two targets in one
	// served list may not claim the same key, or a block on either would
	// silently cover the other.
	CIMDVendorKeys []string `json:"cimd_vendor_keys,omitempty"`

	// OAuthClientIDs match the caller's verified client_id literally, and
	// also match the CIMD catalog URL that admitted it. Naming the catalog
	// URL is how a vendor whose client_id namespace is unbounded — one
	// document minted per MCP server — is still named exactly, since its
	// catalog entry is a wildcard pattern no literal id can reproduce.
	OAuthClientIDs []string `json:"oauth_client_ids,omitempty"`

	// ClientInfoNames match the name an MCP client reports at initialize.
	// Detection only, never authorization: the value is self-reported and
	// any client can claim any name.
	ClientInfoNames []string `json:"client_info_names,omitempty"`
}

// IsZero reports whether no gateway caller is linked to the target. It keeps
// an unlinked target's JSON byte-identical to the shape that predates these
// matchers, which is what leaves the served-list ETag alone.
func (g GatewayClient) IsZero() bool {
	return len(g.CIMDVendorKeys) == 0 && len(g.OAuthClientIDs) == 0 && len(g.ClientInfoNames) == 0
}

// Clone returns a deep copy.
func (g GatewayClient) Clone() GatewayClient {
	return GatewayClient{
		CIMDVendorKeys:  slices.Clone(g.CIMDVendorKeys),
		OAuthClientIDs:  slices.Clone(g.OAuthClientIDs),
		ClientInfoNames: slices.Clone(g.ClientInfoNames),
	}
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

	// GatewayClient names the MCP gateway callers that are this tool.
	// Server-side only: Envelope strips it before agents see it.
	GatewayClient GatewayClient `json:"gateway_client,omitzero"`

	// Enabled is false for targets kept for history but not served.
	Enabled bool `json:"enabled"`
}

// ZeroTarget is the empty target a lookup returns alongside ok=false. It is
// spelled out once here because the linter requires every field of a struct
// literal, and an unresolved lookup is not worth eleven lines at each site.
func ZeroTarget() Target {
	return Target{
		ID:          "",
		DisplayName: "",
		Category:    "",
		Signatures: Signatures{
			BundleIDs:    nil,
			Binaries:     nil,
			ConfigDirs:   nil,
			ProcessNames: nil,
		},
		VersionHint:   nil,
		GatewayClient: GatewayClient{CIMDVendorKeys: nil, OAuthClientIDs: nil, ClientInfoNames: nil},
		Enabled:       false,
	}
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
	out.GatewayClient = t.GatewayClient.Clone()
	return out
}

// Defaults is the code-owned set an empty catalog is seeded from; after that
// Postgres is the source of truth. Derived from the aivendors registry, so a
// product declared once is both admissible and blockable.
//
// A product with no CIMD document carries detection names only, and a vendor
// key covering more than one product names neither — GatewayMatchersFor
// derives both rules.
func Defaults() []Target {
	products := aivendors.ScanProducts()
	targets := make([]Target, 0, len(products))
	for _, product := range products {
		matchers := aivendors.GatewayMatchersFor(product)
		target := Target{
			ID:          product.ID,
			DisplayName: product.DisplayName,
			Category:    Category(product.Category),
			Signatures: Signatures{
				BundleIDs:    orEmpty(product.Signatures.BundleIDs),
				Binaries:     orEmpty(product.Signatures.Binaries),
				ConfigDirs:   orEmpty(product.Signatures.ConfigDirs),
				ProcessNames: orEmpty(product.Signatures.ProcessNames),
			},
			VersionHint: nil,
			GatewayClient: GatewayClient{
				CIMDVendorKeys:  matchers.VendorKeys,
				OAuthClientIDs:  matchers.ClientIDs,
				ClientInfoNames: product.ClientInfoNames,
			},
			Enabled: true,
		}
		if product.VersionPlistKey != "" {
			target.VersionHint = &VersionHint{PlistKey: product.VersionPlistKey}
		}
		targets = append(targets, target)
	}
	return targets
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
		return ZeroTarget(), false
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
		Targets:       wireTargets(s.targets),
	}
}

// wireTargets is targets as agents receive them: gateway-client matchers
// stripped. A device has no use for them, and leaving them in would make a
// matcher-only edit change the ETag and send every agent re-applying a scan
// list that did not move.
func wireTargets(targets []Target) []Target {
	out := make([]Target, 0, len(targets))
	for _, target := range targets {
		cloned := target.Clone()
		cloned.GatewayClient = GatewayClient{CIMDVendorKeys: nil, OAuthClientIDs: nil, ClientInfoNames: nil}
		out = append(out, cloned)
	}
	return out
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
	data, err := json.Marshal(wireTargets(sorted))
	if err != nil {
		_, _ = fmt.Fprintf(hash, "marshal error: %v", err)
	}
	_, _ = hash.Write(data)
	return hex.EncodeToString(hash.Sum(nil))
}
