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
// compiled-in list changes: it is added to every organization's edit counter
// to form the served list_version, so agents re-apply the list.
const DefaultsVersion int32 = 1

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

	// Customized is set on a default the organization has replaced with its
	// own row, for example to disable it.
	Customized bool

	// CreatedAt is when the organization's row was created; zero for an
	// untouched default.
	CreatedAt time.Time

	// UpdatedAt is when the organization's row last changed; zero for an
	// untouched default.
	UpdatedAt time.Time
}

// Overlay applies an organization's rows to the defaults: a row whose id
// matches a default replaces that default and is marked customized, every
// other row is an organization target, and the result is ordered by id.
// Disabled entries are kept so the management surface can show them.
func Overlay(defaults []Target, rows []Entry) []Entry {
	byID := make(map[string]Entry, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	entries := make([]Entry, 0, len(defaults)+len(rows))
	for _, target := range defaults {
		if row, ok := byID[target.ID]; ok {
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
			CreatedAt:  time.Time{},
			UpdatedAt:  time.Time{},
		})
	}
	for _, row := range rows {
		if _, remaining := byID[row.ID]; remaining {
			row.Source = SourceOrganization
			row.Customized = false
			entries = append(entries, row)
		}
	}
	slices.SortFunc(entries, func(a, b Entry) int {
		return strings.Compare(a.ID, b.ID)
	})
	return entries
}

// Served picks the enabled targets out of entries, keeping their order.
func Served(entries []Entry) []Target {
	targets := make([]Target, 0, len(entries))
	for _, entry := range entries {
		if entry.Enabled {
			targets = append(targets, entry.Target)
		}
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

// SameDefinition reports whether two targets describe the same tool, ignoring
// whether it is served. Enabled is excluded deliberately: switching a built-in
// off is the one change an organization may make to it, so it is not part of
// the definition the built-in owns.
func SameDefinition(a, b Target) bool {
	a.Enabled, b.Enabled = false, false
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
	if out.VersionHint != nil && out.VersionHint.PlistKey == "" {
		out.VersionHint = nil
	}
	return out
}

// ListVersion is the served list_version for an organization whose edit
// counter is counter.
func ListVersion(counter int32) int32 {
	return counter + DefaultsVersion
}

// OrganizationList is an organization's full target list and the snapshot
// its agents are served.
type OrganizationList struct {
	// Entries is every target, enabled or not, ordered by id.
	Entries []Entry

	// Snapshot is the enabled targets at the organization's list version.
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
		CreatedAt:  time.Time{},
		UpdatedAt:  time.Time{},
	}, false
}

// LoadOrganizationList reads an organization's rows and edit counter through
// queries and overlays them on the defaults.
func LoadOrganizationList(ctx context.Context, queries *repo.Queries, organizationID string) (*OrganizationList, error) {
	rows, err := queries.ListAIScanTargets(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list ai scan targets: %w", err)
	}
	counter, err := queries.GetAIScanCatalogVersion(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("get ai scan catalog version: %w", err)
	}
	entries := make([]Entry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, EntryFromRow(row))
	}
	overlaid := Overlay(Defaults(), entries)
	return &OrganizationList{
		Entries:  overlaid,
		Snapshot: NewSnapshot(ListVersion(counter), Served(overlaid)),
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
			ID:          row.ID,
			DisplayName: row.DisplayName,
			Category:    Category(row.Category),
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
			Enabled: row.Enabled,
		},
		Source:     SourceOrganization,
		Customized: false,
		CreatedAt:  row.CreatedAt.Time,
		UpdatedAt:  row.UpdatedAt.Time,
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
		DisplayName:     target.DisplayName,
		Category:        string(target.Category),
		BundleIds:       orEmpty(target.Signatures.BundleIDs),
		Binaries:        orEmpty(target.Signatures.Binaries),
		ConfigDirs:      orEmpty(target.Signatures.ConfigDirs),
		ProcessNames:    orEmpty(target.Signatures.ProcessNames),
		VersionPlistKey: plistKey,
		CimdVendorKeys:  orEmpty(target.GatewayClient.CIMDVendorKeys),
		OauthClientIds:  orEmpty(target.GatewayClient.OAuthClientIDs),
		ClientInfoNames: orEmpty(target.GatewayClient.ClientInfoNames),
		Enabled:         target.Enabled,
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
