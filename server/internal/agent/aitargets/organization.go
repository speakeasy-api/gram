package aitargets

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

// DefaultsVersion counts revisions of Defaults. Bump it whenever the
// compiled-in list changes, so agents can tell which defaults they hold.
//
// 2: the registry grew past the original harnesses to cover assistants and
// open model runners, and gained the gateway-client matchers.
//
// 3: ChatGPT moved from harness to assistant, and Cline gained the versioned
// VS Code extension config dirs that detect an install without the npm CLI.
//
// 4: ChatGPT Classic gained its process name, so an agent reports it as
// running and not merely installed.
//
// 5: Pi joined the catalog, so a device running it no longer scans clean.
const DefaultsVersion int32 = 5

// Source says where a target in an organization's list comes from.
type Source string

const (
	// SourceDefault marks a target compiled into Gram.
	SourceDefault Source = "default"

	// SourceOrganization marks a target the organization added.
	SourceOrganization Source = "organization"
)

// Entry is one target in an organization's list with its provenance and,
// for rows the organization owns, the row's timestamps.
type Entry struct {
	Target

	// Source is where the target comes from.
	Source Source

	// Customized is set on a built-in the organization has a row for, which
	// now means one thing: it has recorded an access decision about it. The
	// row never carries a definition, so a built-in's definition is never
	// customized in the literal sense.
	Customized bool

	// Decision is the organization's standing access decision for the target.
	// Unreviewed for one it has said nothing about.
	Decision DecisionRecord

	// CreatedAt is when the organization's row was created; zero for an
	// untouched default.
	CreatedAt time.Time

	// UpdatedAt is when the organization's row last changed; zero for an
	// untouched default.
	UpdatedAt time.Time
}

// Overlay applies an organization's rows to the defaults: a row whose id
// matches a built-in carries that organization's choices about it, every
// other row is a target the organization added, and the result is ordered by
// id. Every entry is in the organization's inventory and therefore probed
// for; there is no inert entry to keep around.
func Overlay(defaults []Target, rows []Entry) []Entry {
	byID := make(map[string]Entry, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	entries := make([]Entry, 0, len(defaults)+len(rows))
	for _, target := range defaults {
		if row, ok := byID[target.ID]; ok {
			// The built-in's own definition wins. A row under its id records
			// only the decision the organization has made about it, so a later
			// registry revision still reaches an organization that has touched
			// this one. The decision lives on the Entry rather than the
			// embedded Target, so replacing the definition keeps it.
			row.Target = target.Clone()
			row.Source = SourceDefault
			row.Customized = true
			entries = append(entries, row)
			delete(byID, target.ID)
			continue
		}
		entries = append(entries, Entry{
			Target:     target.Clone(),
			Source:     SourceDefault,
			Customized: false,
			Decision:   UnreviewedDecisionRecord(target.ID),
			CreatedAt:  time.Time{},
			UpdatedAt:  time.Time{},
		})
	}
	for _, row := range rows {
		if _, remaining := byID[row.ID]; !remaining {
			continue
		}
		// A row matching no default is an organization's own target, and its
		// row carries the full definition. Except when it does not: a built-in
		// that has since been removed from the registry leaves behind the
		// decision-only row an organization wrote about it, whose definition
		// columns were always empty. Promoting that to an organization target
		// would serve agents a target with no name, no category and no
		// signatures, and keep them probing for a product the registry no
		// longer ships. Skip it: the row records a decision about something
		// that no longer exists, and purging it is part of removing the
		// built-in.
		if strings.TrimSpace(row.DisplayName) == "" {
			continue
		}
		row.Source = SourceOrganization
		row.Customized = false
		entries = append(entries, row)
	}
	slices.SortFunc(entries, func(a, b Entry) int {
		return strings.Compare(a.ID, b.ID)
	})
	return entries
}

// Served is every target in the organization's inventory, in order.
//
// Being in the inventory is what makes a target probed for; there is no
// second switch. A built-in leaves the inventory by being deleted from the
// registry, which reaches agents on their next poll, and an organization's
// own target leaves by having its row deleted.
func Served(entries []Entry) []Target {
	targets := make([]Target, 0, len(entries))
	for _, entry := range entries {
		targets = append(targets, entry.Target)
	}
	return targets
}

// DefaultByID resolves a compiled-in default by id.
func DefaultByID(id string) (Target, bool) {
	for _, target := range Defaults() {
		if target.ID == id {
			return target, true
		}
	}
	return ZeroTarget(), false
}

// ResolveBuiltinDefinition puts a built-in's compiled-in definition back over
// a target read straight from its row, keeping the one thing the row owns:
// whether the organization has it switched on.
//
// A row under a built-in's id stores only the organization's choices, so its
// definition columns are empty. Overlay already does this when it composes the
// served list, and every other reader of a single row needs the same thing.
// Without it a caller sees a built-in with no display name, no category, no
// signatures and no gateway matchers, and takes that emptiness for the
// target's definition: a toggle then fails SameDefinition against the real
// built-in, and an audit snapshot records a blank target.
//
// Returns the target unchanged when its id names no built-in.
func ResolveBuiltinDefinition(target Target) Target {
	builtin, isBuiltin := DefaultByID(target.ID)
	if !isBuiltin {
		return target
	}
	return builtin.Clone()
}

// SameDefinition reports whether two targets describe the same tool. Every
// field a target carries is part of its definition now, so the comparison is
// total: there is no per-organization state left to exclude from it.
func SameDefinition(a, b Target) bool {
	return reflect.DeepEqual(normalizeForComparison(a), normalizeForComparison(b))
}

// normalizeForComparison folds the representations that mean the same thing —
// a nil list and an empty one — so a client that round-trips a built-in
// through JSON is not accused of editing it.
func normalizeForComparison(t Target) Target {
	out := t.Clone()
	out.Signatures = Signatures{
		BundleIDs:    orEmpty(out.Signatures.BundleIDs),
		Binaries:     orEmpty(out.Signatures.Binaries),
		ConfigDirs:   orEmpty(out.Signatures.ConfigDirs),
		ProcessNames: orEmpty(out.Signatures.ProcessNames),
	}
	out.GatewayClient = GatewayClient{
		CIMDVendorKeys:  orEmpty(out.GatewayClient.CIMDVendorKeys),
		OAuthClientIDs:  orEmpty(out.GatewayClient.OAuthClientIDs),
		ClientInfoNames: orEmpty(out.GatewayClient.ClientInfoNames),
	}
	// An omitted hint and an explicit DefaultVersionPlistKey mean the same
	// thing, so a client that round-trips a built-in and sends the default back
	// is not accused of redefining it.
	if out.VersionHint != nil && (out.VersionHint.PlistKey == "" || out.VersionHint.PlistKey == DefaultVersionPlistKey) {
		out.VersionHint = nil
	}
	return out
}

// ListVersion is the served list_version: the compiled-in defaults revision.
// Organization edits do not move it — they change the served targets, which
// the snapshot ETag hashes, and that is what makes an agent re-apply.
func ListVersion() int32 {
	return DefaultsVersion
}

// OrganizationList is an organization's full target list and the snapshot
// its agents are served.
type OrganizationList struct {
	// Entries is every target in the organization's inventory, ordered by id.
	Entries []Entry

	// Snapshot is the organization's targets at its list version.
	Snapshot *Snapshot
}

// Entry resolves one entry by id.
func (l *OrganizationList) Entry(id string) (Entry, bool) {
	for _, entry := range l.Entries {
		if entry.ID == id {
			return entry, true
		}
	}
	return Entry{
		Target:     ZeroTarget(),
		Source:     "",
		Customized: false,
		Decision:   UnreviewedDecisionRecord(id),
		CreatedAt:  time.Time{},
		UpdatedAt:  time.Time{},
	}, false
}

// LoadOrganizationList reads an organization's rows and overlays them on the
// defaults.
func LoadOrganizationList(ctx context.Context, queries *repo.Queries, organizationID string) (*OrganizationList, error) {
	rows, err := queries.ListAIScanTargets(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list ai scan targets: %w", err)
	}
	entries := make([]Entry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, EntryFromRow(row))
	}
	overlaid := Overlay(Defaults(), entries)
	return &OrganizationList{
		Entries:  overlaid,
		Snapshot: NewSnapshot(ListVersion(), Served(overlaid)),
	}, nil
}

// EntryFromRow converts an organization's row into an Entry. Overlay settles
// Source and Customized.
func EntryFromRow(row repo.AiScanTarget) Entry {
	var hint *VersionHint
	if row.VersionPlistKey.Valid && row.VersionPlistKey.String != "" {
		hint = &VersionHint{PlistKey: row.VersionPlistKey.String}
	}
	return Entry{
		Target: Target{
			ID: row.ID,
			// Null on a row that only carries choices about a built-in.
			// Overlay puts the built-in's own definition back over the top.
			DisplayName: row.DisplayName.String,
			Category:    Category(row.Category.String),
			Signatures: Signatures{
				BundleIDs:    orEmpty(row.BundleIds),
				Binaries:     orEmpty(row.Binaries),
				ConfigDirs:   orEmpty(row.ConfigDirs),
				ProcessNames: orEmpty(row.ProcessNames),
			},
			VersionHint: hint,
			// Empty matcher lists stay nil rather than becoming empty
			// slices, so an unlinked target reports IsZero and keeps the
			// served-list ETag where it was.
			GatewayClient: GatewayClient{
				CIMDVendorKeys:  orNil(row.CimdVendorKeys),
				OAuthClientIDs:  orNil(row.OauthClientIds),
				ClientInfoNames: orNil(row.ClientInfoNames),
			},
		},
		Source:     SourceOrganization,
		Customized: false,
		Decision: DecisionRecord{
			TargetID:  row.ID,
			Decision:  Decision(row.Status),
			Rationale: row.Rationale.String,
		},
		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
	}
}

// UpsertParams maps a validated target onto the organization's upsert query.
func UpsertParams(organizationID string, target Target) repo.UpsertAIScanTargetParams {
	plistKey := pgtype.Text{String: "", Valid: false}
	if target.VersionHint != nil {
		plistKey = conv.ToPGTextEmpty(target.VersionHint.PlistKey)
	}
	return repo.UpsertAIScanTargetParams{
		OrganizationID:  organizationID,
		ID:              target.ID,
		DisplayName:     conv.ToPGTextEmpty(target.DisplayName),
		Category:        conv.ToPGTextEmpty(string(target.Category)),
		BundleIds:       orEmpty(target.Signatures.BundleIDs),
		Binaries:        orEmpty(target.Signatures.Binaries),
		ConfigDirs:      orEmpty(target.Signatures.ConfigDirs),
		ProcessNames:    orEmpty(target.Signatures.ProcessNames),
		VersionPlistKey: plistKey,
		CimdVendorKeys:  orEmpty(target.GatewayClient.CIMDVendorKeys),
		OauthClientIds:  orEmpty(target.GatewayClient.OAuthClientIDs),
		ClientInfoNames: orEmpty(target.GatewayClient.ClientInfoNames),
	}
}

// orEmpty keeps nil slices out of JSON and Postgres.
func orEmpty(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// orNil is the inverse: an empty column reads back as nil, which is what
// GatewayClient.IsZero tests.
func orNil(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return values
}
