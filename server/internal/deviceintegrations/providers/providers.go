// Package providers defines the vendor abstraction for device integrations:
// a registry of descriptors, capability interfaces for inventory sources
// (MDMs pulled for the managed-device fleet) and evidence sinks (compliance
// platforms pushed agent-coverage evidence), and the credential/settings
// specs that drive dashboard form rendering and payload validation.
//
// Adding a vendor means implementing one of the capability interfaces in a
// subpackage and registering a Descriptor from that package's init. The
// framework (store, management API, Temporal scheduling) needs no changes.
package providers

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/speakeasy-api/gram/server/internal/guardian"
)

// Capability names one integration direction a provider supports. A provider
// declares capabilities in its Descriptor and must implement the matching
// interface; the compiler enforces the contract via the constructor's return
// type assertions in the registry.
type Capability string

const (
	// CapabilityInventorySource marks a provider that pulls the managed-device
	// fleet from the vendor (MDMs such as Jamf or Kandji).
	CapabilityInventorySource Capability = "inventory_source"
	// CapabilityEvidenceSink marks a provider that pushes agent-coverage
	// evidence into the vendor (compliance platforms such as Drata or Vanta).
	CapabilityEvidenceSink Capability = "evidence_sink"
)

// FieldKind hints how the dashboard should render a credential or settings
// field. Free-form so new kinds need no framework change.
type FieldKind string

const (
	FieldKindText FieldKind = "text"
	FieldKindURL  FieldKind = "url"
)

// CredentialField describes one field a provider needs to connect. Secret
// fields are stored in the encrypted, write-only credentials blob; non-secret
// fields land in the readable settings document so the dashboard can
// redisplay them.
type CredentialField struct {
	// Key is the field's identifier in the credentials or settings document.
	Key string
	// Label is the human-readable name the dashboard renders.
	Label string
	// Kind hints the input widget (text, url).
	Kind FieldKind
	// Secret routes the field into the encrypted write-only blob rather than
	// the readable settings document.
	Secret bool
	// Required makes upsert validation reject configs missing the field.
	Required bool
}

// ScheduleSpec declares one sync pipeline a provider runs. Cadence lives here
// in code — deliberately not in the database — so polling behavior ships as
// code changes and orgs cannot drift into pathological intervals.
type ScheduleSpec struct {
	// Schedule is the pipeline discriminator stored on
	// device_integration_schedules rows (e.g. 'jamf_inventory').
	Schedule string
	// Capability names the pipeline this schedule drives. The sync runner
	// dispatches on the FIRED schedule's capability — never on the provider's
	// capability set — so two schedules can never silently run the same
	// pipeline twice.
	Capability Capability
	// Interval is the target time between successful runs.
	Interval time.Duration
}

// Credentials is the decrypted secret material for one config, keyed by
// CredentialField.Key. Values never appear in logs, traces, Temporal
// payloads, or API responses.
type Credentials map[string]string

// Settings is the non-secret, admin-visible configuration for one config,
// keyed by CredentialField.Key.
type Settings map[string]string

// Device is the normalized device record an InventorySource yields. Fields
// map to mdm_devices columns; Raw carries the vendor's full record.
type Device struct {
	// ExternalID is the vendor's identifier for the device, unique within one
	// config.
	ExternalID string
	// SerialNumber is the hardware serial as reported by the vendor.
	SerialNumber string
	Hostname     string
	OSName       string
	OSVersion    string
	// UserEmail is the assigned user's email exactly as the vendor reported
	// it; empty when the vendor has no assignment.
	UserEmail string
	// LastCheckInAt is the vendor's last device check-in time; zero when the
	// vendor doesn't report one.
	LastCheckInAt time.Time
	// Raw is the vendor's full device record, persisted as-is for debugging
	// and future field promotion.
	Raw []byte
}

// DevicePage is one page of an inventory listing.
type DevicePage struct {
	Devices []Device
	// NextCursor requests the next page; empty means the listing is complete.
	NextCursor string
}

// AgentAttestation names which claim a piece of coverage evidence supports.
// It is per DEVICE, not per organization: even with device-level matching
// enabled, a machine whose agent cannot report a serial is matched by its
// assigned user's email and is therefore only user-attested. It therefore
// travels with every pushed record, and the coverage API downgrades a whole
// response to the weaker value when any active device carries it.
type AgentAttestation string

const (
	// AttestationDevice means the agent on THIS machine reported in — matched
	// on hardware serial.
	AttestationDevice AgentAttestation = "device"
	// AttestationUser means only that the device's assigned user runs the
	// agent somewhere — matched on assigned-user email. Strictly weaker.
	AttestationUser AgentAttestation = "user"
)

// CoverageDevice is one device's entry in an evidence snapshot.
//
// Attestation is carried as data rather than encoded in field names. Naming
// the fields for one claim (assignedUserAgentActive, or deviceAgentActive)
// forces every record into that claim's strength, which is wrong in both
// directions: the former undersells a serial-matched device, and the latter
// asserts of an email-matched one something we cannot prove. A stable schema
// plus an explicit AgentAttestation lets an auditor see exactly which
// evidence backs each row.
type CoverageDevice struct {
	ExternalID   string
	SerialNumber string
	Hostname     string
	UserEmail    string
	// AgentActive reports whether the attested agent heartbeat is within the
	// freshness window. Read it together with AgentAttestation: what it
	// asserts depends on which match produced it.
	AgentActive bool
	// AgentAttestation is the strength of AgentActive for this device.
	AgentAttestation AgentAttestation
	// AgentLastSeenAt is the attested agent's latest heartbeat; zero when no
	// agent has ever synced for this device or its assigned user.
	AgentLastSeenAt time.Time
}

// CoverageSnapshot is the full evidence set an EvidenceSink pushes. Pushes
// are snapshot-replace: each push represents the complete current state.
type CoverageSnapshot struct {
	OrganizationID string
	GeneratedAt    time.Time
	Devices        []CoverageDevice
}

// Deps carries the framework services a provider may use. All outbound HTTP
// must go through Client: it is guardian's SSRF-hardened client, and instance
// URLs are customer-supplied.
type Deps struct {
	// Client is the SSRF-hardened HTTP client from the guardian policy.
	Client *guardian.HTTPClient
}

// InventorySource pulls the managed-device fleet from the vendor.
//
// Snapshot consistency: a paginated listing need not be a perfectly
// consistent snapshot — the framework tolerates a device transiently absent
// from one completed pull. Mark-missing runs only after a pull completes and
// clears on the next pull that sees the device, so a vendor whose paging can
// drop a device under mid-pull churn (offset pagination) mis-marks it until
// a later pull fetches it — one sync interval per churn event in the common
// case, though sustained churn can keep re-skipping the same device.
// Providers relying on this tolerance must say so at their pagination
// logic; anyone strengthening what a completed pull implies (partial-pull
// marking, same-cycle alerting or evidence export) must revisit those
// providers first.
type InventorySource interface {
	// TestConnection validates the credentials/settings with a minimal read.
	// Implementations must bound their own request sizes; the caller bounds
	// the deadline via ctx.
	TestConnection(ctx context.Context, creds Credentials, settings Settings) error
	// ListDevices returns one page of the vendor's device inventory. An empty
	// cursor requests the first page.
	ListDevices(ctx context.Context, creds Credentials, settings Settings, cursor string) (DevicePage, error)
}

// EvidenceSink pushes agent-coverage evidence into the vendor.
type EvidenceSink interface {
	// TestConnection validates the credentials/settings with a minimal call.
	TestConnection(ctx context.Context, creds Credentials, settings Settings) error
	// PushCoverage replaces the vendor-side evidence with the snapshot.
	// Implementations must be idempotent: a retried push of the same snapshot
	// must not duplicate records.
	PushCoverage(ctx context.Context, creds Credentials, settings Settings, snapshot CoverageSnapshot) error
}

// Provisioner is an OPTIONAL capability a source or sink may also implement to
// perform one-time vendor-side setup during connect — creating the object it
// will read from or push to (e.g. a Drata Custom Connection) so the customer
// does not have to hand-craft it against the vendor API. The framework calls
// Provision during the config upsert, after credentials and settings are
// merged with any stored values, and stores the returned Settings — so a
// provider can hand back the id of whatever it created (a connection id, a
// resource id) for later syncs to use. orgID is the Gram organization the
// config belongs to; encode it into the vendor object's identity so two Gram
// orgs that share one vendor account each own a distinct object.
//
// Two hard requirements:
//   - Idempotent (find-or-create): a re-save must reuse the existing vendor
//     object, never create a duplicate. Return the settings unchanged when
//     nothing needs provisioning. The org-scoped identity above is also what
//     keeps this correct across orgs: the Gram-side advisory lock only
//     serializes one (org, provider), so nothing stops two different orgs
//     sharing a vendor account from provisioning concurrently.
//   - Self-contained on the vendor: Provision must work from only the orgID,
//     creds, and settings it is handed. It runs inside the config-upsert
//     transaction (under the same advisory lock that serializes upserts), so it
//     must not read Gram-side state that the not-yet-committed transaction would
//     not see, and it must not block on slow vendor I/O longer than the upsert
//     can hold that lock — see provisionTimeout in impl.go.
type Provisioner interface {
	Provision(ctx context.Context, orgID string, creds Credentials, settings Settings) (Settings, error)
}

// Descriptor declares one vendor to the framework.
type Descriptor struct {
	// ID is the provider discriminator stored on device_integration_configs
	// rows (e.g. 'jamf').
	ID string
	// DisplayName is the human-readable vendor name.
	DisplayName string
	// Capabilities the provider implements. Must be non-empty.
	Capabilities []Capability
	// Fields describes the credential and settings inputs the vendor needs.
	Fields []CredentialField
	// Schedules declares the sync pipelines the provider runs.
	Schedules []ScheduleSpec
	// NewInventorySource constructs the pull implementation. Required exactly
	// when Capabilities contains CapabilityInventorySource.
	NewInventorySource func(deps Deps) InventorySource
	// NewEvidenceSink constructs the push implementation. Required exactly
	// when Capabilities contains CapabilityEvidenceSink.
	NewEvidenceSink func(deps Deps) EvidenceSink
}

// HasCapability reports whether the descriptor declares the capability.
func (d Descriptor) HasCapability(c Capability) bool {
	return slices.Contains(d.Capabilities, c)
}

// SecretFields returns the descriptor's secret fields.
func (d Descriptor) SecretFields() []CredentialField {
	fields := make([]CredentialField, 0, len(d.Fields))
	for _, f := range d.Fields {
		if f.Secret {
			fields = append(fields, f)
		}
	}
	return fields
}

func (d Descriptor) validate() error {
	if d.ID == "" {
		return fmt.Errorf("provider descriptor missing id")
	}
	if d.DisplayName == "" {
		return fmt.Errorf("provider %q descriptor missing display name", d.ID)
	}
	if len(d.Capabilities) == 0 {
		return fmt.Errorf("provider %q declares no capabilities", d.ID)
	}
	seenCaps := make(map[Capability]bool, len(d.Capabilities))
	for _, c := range d.Capabilities {
		if c != CapabilityInventorySource && c != CapabilityEvidenceSink {
			return fmt.Errorf("provider %q declares unknown capability %q", d.ID, c)
		}
		if seenCaps[c] {
			return fmt.Errorf("provider %q declares capability %q twice", d.ID, c)
		}
		seenCaps[c] = true
	}
	if len(d.Schedules) == 0 {
		return fmt.Errorf("provider %q declares no schedules", d.ID)
	}
	if d.HasCapability(CapabilityInventorySource) != (d.NewInventorySource != nil) {
		return fmt.Errorf("provider %q inventory_source capability and constructor must be declared together", d.ID)
	}
	if d.HasCapability(CapabilityEvidenceSink) != (d.NewEvidenceSink != nil) {
		return fmt.Errorf("provider %q evidence_sink capability and constructor must be declared together", d.ID)
	}
	seen := make(map[string]bool, len(d.Fields))
	for _, f := range d.Fields {
		if f.Key == "" || f.Label == "" {
			return fmt.Errorf("provider %q has a field missing key or label", d.ID)
		}
		if seen[f.Key] {
			return fmt.Errorf("provider %q declares duplicate field %q", d.ID, f.Key)
		}
		seen[f.Key] = true
	}
	seenSchedules := make(map[string]bool, len(d.Schedules))
	seenCapabilities := make(map[Capability]bool, len(d.Schedules))
	for _, s := range d.Schedules {
		if s.Schedule == "" || s.Interval < time.Minute || s.Interval%time.Minute != 0 {
			// The catalog renders whole minutes; enforcing whole-minute
			// intervals keeps the displayed cadence exactly the real one.
			return fmt.Errorf("provider %q schedule %q needs a name and a whole-minute interval of at least one minute", d.ID, s.Schedule)
		}
		if seenSchedules[s.Schedule] {
			return fmt.Errorf("provider %q declares duplicate schedule %q", d.ID, s.Schedule)
		}
		seenSchedules[s.Schedule] = true
		if !d.HasCapability(s.Capability) {
			return fmt.Errorf("provider %q schedule %q names capability %q the provider does not declare", d.ID, s.Schedule, s.Capability)
		}
		// One schedule per capability: a second schedule for the same
		// capability would run the identical pipeline twice, racing its own
		// mark-missing pass.
		if seenCapabilities[s.Capability] {
			return fmt.Errorf("provider %q declares two schedules for capability %q", d.ID, s.Capability)
		}
		seenCapabilities[s.Capability] = true
	}
	return nil
}

var (
	registryMu sync.RWMutex
	registry   = map[string]Descriptor{}
)

// Register adds a provider to the registry. Vendor packages call this from
// init; a duplicate or invalid descriptor panics because it is a programming
// error caught at startup, not a runtime condition. Slice fields are cloned
// so later mutation of the caller's descriptor cannot bypass validation.
func Register(d Descriptor) {
	if err := d.validate(); err != nil {
		panic(fmt.Sprintf("deviceintegrations: %v", err))
	}
	d = d.clone()
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[d.ID]; exists {
		panic(fmt.Sprintf("deviceintegrations: provider %q registered twice", d.ID))
	}
	registry[d.ID] = d
}

// clone copies the descriptor's slice fields so registry entries and the
// descriptors handed to readers are isolated from caller mutation.
func (d Descriptor) clone() Descriptor {
	d.Capabilities = slices.Clone(d.Capabilities)
	d.Fields = slices.Clone(d.Fields)
	d.Schedules = slices.Clone(d.Schedules)
	return d
}

// Lookup returns the descriptor for a provider id.
func Lookup(id string) (Descriptor, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	d, ok := registry[id]
	if !ok {
		return Descriptor{}, false //nolint:exhaustruct // zero descriptor for the not-found case
	}
	return d.clone(), true
}

// All returns every registered descriptor, sorted by id for stable listings.
func All() []Descriptor {
	registryMu.RLock()
	defer registryMu.RUnlock()
	all := make([]Descriptor, 0, len(registry))
	for _, d := range registry {
		all = append(all, d.clone())
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	return all
}

// AuthError marks a vendor rejection of the stored credentials (401/403
// class). The sync runner counts these toward the auto-pause threshold so a
// revoked credential becomes a visible paused schedule instead of an
// infinite retry against the vendor API. Wrap with NewAuthError from vendor
// implementations.
type AuthError struct {
	Err error
}

func NewAuthError(err error) *AuthError {
	return &AuthError{Err: err}
}

func (e *AuthError) Error() string {
	return "provider rejected credentials: " + e.Err.Error()
}

func (e *AuthError) Unwrap() error {
	return e.Err
}

// IsAuthError reports whether any error in the chain is a credential
// rejection.
func IsAuthError(err error) bool {
	var authErr *AuthError
	return errors.As(err, &authErr)
}
