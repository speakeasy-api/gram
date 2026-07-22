package remotesessions

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// Issuer migration (AIS-290) consolidates two remote_session_issuers that
// describe the same upstream authorization server. It re-points every active
// remote_session_client off the source issuer and onto the target, then
// soft-deletes the source.
//
// No user re-authenticates, because the operation is a foreign-key re-point
// rather than a copy of any credential:
//
//   - remote_session_clients is the only table with a foreign key to
//     remote_session_issuers, and remote_sessions reference the client, so a
//     client's sessions and its user_session_issuer bindings travel with it.
//   - Tokens are encrypted under one global key, not a per-issuer key, so the
//     stored ciphertext stays decryptable.
//   - client_id/client_secret belong to the upstream URL, not to Gram's issuer
//     row, so refresh tokens stay bound to an unchanged client_id.
//
// Ordering inside the transaction is load-bearing: re-point before
// soft-deleting the source, because the runtime resolution query filters
// `i.deleted IS FALSE` and would stop resolving any client still pointing at a
// tombstoned issuer.

// issuerScope ranks an issuer by the breadth of the tenancy it serves. A
// migration may only move clients onto an issuer that is at least as broad as
// the one they leave, so a client never loses visibility of its issuer.
type issuerScope int

const (
	// issuerScopeProject is a project-specific issuer (project_id set), visible
	// only to that project.
	issuerScopeProject issuerScope = iota
	// issuerScopeOrganization is an organization-level issuer (project_id NULL,
	// organization_id set), inherited by every project in the organization.
	issuerScopeOrganization
	// issuerScopeGlobal is a platform-level issuer (both NULL), visible to every
	// organization. No org-admin surface can load or list one today — see
	// GetGlobalRemoteSessionIssuerByID and ListGlobalRemoteSessionIssuers, which
	// are reachable only from the platform-admin handlers — so migrateIssuer
	// cannot currently target one. The rank exists so the deferred platform-admin
	// migrate endpoint slots in without reworking the ladder.
	issuerScopeGlobal
)

func scopeOf(issuer repo.RemoteSessionIssuer) issuerScope {
	switch {
	case issuer.ProjectID.Valid:
		return issuerScopeProject
	case issuer.OrganizationID.Valid:
		return issuerScopeOrganization
	default:
		return issuerScopeGlobal
	}
}

func (s issuerScope) String() string {
	switch s {
	case issuerScopeProject:
		return "project-specific"
	case issuerScopeOrganization:
		return "organization-level"
	case issuerScopeGlobal:
		return "platform-level"
	default:
		return "unknown"
	}
}

// migrationScopeError reports a source/target pair the scope ladder forbids. The
// handler maps it to a 400: the request names two real issuers, but no
// migration between them is defined.
type migrationScopeError struct{ reason string }

func (e migrationScopeError) Error() string { return e.reason }

// validateMigrationScope enforces the tenancy ladder: a migration may move
// "upward" to a broader scope (project to organization, project or organization
// to platform) or "sideways" within the same tenant (project to the same
// project, organization to the same organization). It may never move downward
// into a narrower scope, nor sideways across tenants.
//
// Cross-tenant migration is forbidden outright. Moving downward is forbidden
// because the runtime resolution query joins the issuer only for endpoint
// metadata and never filters on the issuer's own project_id: re-pointing a
// project A client onto a project B issuer would keep resolving at runtime while
// listing that client under another project's issuer in the org-admin UI.
func validateMigrationScope(source, target repo.RemoteSessionIssuer) error {
	sourceScope, targetScope := scopeOf(source), scopeOf(target)

	if targetScope < sourceScope {
		return migrationScopeError{fmt.Sprintf(
			"cannot migrate a %s issuer onto a %s issuer: the target must be at least as broad as the source",
			sourceScope, targetScope,
		)}
	}

	if sourceScope != targetScope {
		// Moving upward. The broader target must still contain the source's
		// tenant: an organization-level target has to belong to the source's
		// organization. A platform-level target belongs to no tenant and
		// contains every one of them, so there is nothing further to check.
		if targetScope == issuerScopeOrganization && source.OrganizationID.String != target.OrganizationID.String {
			return migrationScopeError{"cannot migrate an issuer into another organization"}
		}
		return nil
	}

	// Moving sideways. Both issuers must name the same tenant.
	switch sourceScope {
	case issuerScopeProject:
		if source.ProjectID.UUID != target.ProjectID.UUID {
			return migrationScopeError{"cannot migrate an issuer into another project; move it to the target project first, or migrate onto an organization-level issuer"}
		}
		if source.OrganizationID.String != target.OrganizationID.String {
			return migrationScopeError{"cannot migrate an issuer into another organization"}
		}
	case issuerScopeOrganization:
		if source.OrganizationID.String != target.OrganizationID.String {
			return migrationScopeError{"cannot migrate an issuer into another organization"}
		}
	case issuerScopeGlobal:
		// Two platform-level issuers share the single platform tenant.
	}

	return nil
}

// endpointMismatches names the authorization-server metadata fields that differ
// between source and target. Any difference blocks the migration: the migrated
// clients' live sessions were established against the source's endpoints, and
// silently re-pointing them at a different authorization server would break
// token refresh without the user ever being asked to re-authenticate.
//
// A field is equal when both sides are unset or both are set to the same value.
// One side set and the other unset is a mismatch, not a match, so a target that
// merely omits an endpoint the source declares cannot absorb its clients.
func endpointMismatches(source, target repo.RemoteSessionIssuer) []string {
	var mismatches []string

	if source.Issuer != target.Issuer {
		mismatches = append(mismatches, "issuer")
	}
	if !pgTextEqual(source.TokenEndpoint, target.TokenEndpoint) {
		mismatches = append(mismatches, "token_endpoint")
	}
	if !pgTextEqual(source.AuthorizationEndpoint, target.AuthorizationEndpoint) {
		mismatches = append(mismatches, "authorization_endpoint")
	}

	return mismatches
}

func pgTextEqual(a, b pgtype.Text) bool {
	if a.Valid != b.Valid {
		return false
	}
	return !a.Valid || a.String == b.String
}

// migrationWarnings names issuer fields that diverge without blocking the
// migration. The target's values become authoritative for every migrated
// client, so these are surfaced in the preflight for the admin to accept.
//
// These are advisory rather than blocking by design, but they are not inert:
// the runtime resolution query reads oidc, passthrough, and scopes_supported off
// the issuer, so a divergent target does change how already-authenticated
// sessions refresh and exchange tokens. The preflight is the only place an admin
// sees that before it happens.
func migrationWarnings(source, target repo.RemoteSessionIssuer) []string {
	var warnings []string

	if source.Oidc != target.Oidc {
		warnings = append(warnings, fmt.Sprintf("oidc changes from %t to %t for migrated clients", source.Oidc, target.Oidc))
	}
	if source.Passthrough != target.Passthrough {
		warnings = append(warnings, fmt.Sprintf("passthrough changes from %t to %t for migrated clients", source.Passthrough, target.Passthrough))
	}
	if !slices.Equal(source.ScopesSupported, target.ScopesSupported) {
		warnings = append(warnings, "scopes_supported differs; the target issuer's scopes become authoritative")
	}

	return warnings
}

// migratePreflight is the impact summary shared by getIssuerMigratePreflight and
// by migrateIssuer's own guards, so the dialog an admin confirms and the
// mutation that runs cannot disagree about what blocks a migration.
type migratePreflight struct {
	clientCount               int64
	mcpServerNames            []string
	endpointMismatches        []string
	conflictingMcpServerNames []string
	warnings                  []string
}

func (p migratePreflight) canMigrate() bool {
	return len(p.endpointMismatches) == 0 && len(p.conflictingMcpServerNames) == 0
}

// buildMigratePreflight computes every blocker and impact figure for migrating
// source onto target. The caller has already loaded both issuers scoped to the
// organization and validated the scope ladder.
func buildMigratePreflight(ctx context.Context, r *repo.Queries, source, target repo.RemoteSessionIssuer) (migratePreflight, error) {
	clientCount, err := r.CountRemoteSessionClientsByIssuerID(ctx, source.ID)
	if err != nil {
		return migratePreflight{}, fmt.Errorf("count source issuer clients: %w", err)
	}

	nameRows, err := r.ListOrganizationMcpServerNamesForIssuer(ctx, source.ID)
	if err != nil {
		return migratePreflight{}, fmt.Errorf("list mcp server names for source issuer: %w", err)
	}
	names := make([]string, 0, len(nameRows))
	for _, row := range nameRows {
		names = append(names, orgDisplayName(conv.FromPGText[string](row.Name), row.Url))
	}

	conflictRows, err := r.ListConflictingClientBindingsForIssuerMigration(ctx, repo.ListConflictingClientBindingsForIssuerMigrationParams{
		SourceIssuerID: source.ID,
		TargetIssuerID: target.ID,
	})
	if err != nil {
		return migratePreflight{}, fmt.Errorf("detect conflicting client bindings: %w", err)
	}

	// A conflicting user_session_issuer that gates no MCP server, or one whose
	// server has neither a name nor a URL to show, still blocks the migration. So
	// fall back to its id rather than emitting a blank label: a caller that saw an
	// empty entry beside a 409 would have nothing to act on.
	conflicts := make([]string, 0, len(conflictRows))
	for _, row := range conflictRows {
		label := orgDisplayName(conv.FromPGText[string](row.McpServerName), row.McpServerUrl)
		if strings.TrimSpace(label) == "" {
			label = fmt.Sprintf("user session issuer %s", row.UserSessionIssuerID)
		}
		conflicts = append(conflicts, label)
	}
	sort.Strings(conflicts)
	conflicts = slices.Compact(conflicts)

	return migratePreflight{
		clientCount:               clientCount,
		mcpServerNames:            names,
		endpointMismatches:        endpointMismatches(source, target),
		conflictingMcpServerNames: conflicts,
		warnings:                  migrationWarnings(source, target),
	}, nil
}

// lockIssuersForMigration takes the transaction-scoped advisory locks that
// serialize a re-point against a concurrent client attach on either issuer.
// Locks are taken in ascending issuer id order so two concurrent migrations
// touching the same pair cannot deadlock.
func lockIssuersForMigration(ctx context.Context, r *repo.Queries, issuerIDs ...uuid.UUID) error {
	ordered := slices.Clone(issuerIDs)
	slices.SortFunc(ordered, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })

	for _, id := range ordered {
		if err := r.LockRemoteSessionIssuerForClientBinding(ctx, id); err != nil {
			return fmt.Errorf("lock remote session issuer %s for client binding: %w", id, err)
		}
	}

	return nil
}
