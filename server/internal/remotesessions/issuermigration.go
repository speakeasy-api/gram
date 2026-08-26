package remotesessions

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
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
	// organization. Only the platform-admin migrate surface may name one: the
	// org-admin surface loads both ends through org-scoped queries, so a global
	// issuer is not addressable there at all.
	issuerScopeGlobal
)

func scopeOf(issuer repo.RemoteSessionIssuer) issuerScope {
	return scopeOfTenancy(issuer.ProjectID, issuer.OrganizationID)
}

// scopeOfTenancy ranks a row from its two tenancy columns alone, for callers
// holding a narrow projection rather than a whole issuer record. scopeOf is the
// form to reach for when a full record is in hand; both must agree, so the
// ladder is defined once, here.
func scopeOfTenancy(projectID uuid.NullUUID, organizationID pgtype.Text) issuerScope {
	switch {
	case projectID.Valid:
		return issuerScopeProject
	case organizationID.Valid:
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
//
// The issuer identifier is compared canonically while the two endpoints are
// compared literally. Only the issuer is an identity: RFC 8414 makes it the name
// of the authorization server, and two spellings that differ solely by a
// trailing slash or an explicit default port name the same one. Duplicates that
// differ that way are the population consolidation exists to clean up, and
// discovery finds candidates by the same canonical equality, so comparing the
// issuer literally here would surface candidates this check could only ever
// refuse. The endpoints are request targets rather than identities, so they stay
// literal: an equivalent-but-differently-spelled endpoint changes nothing about
// where tokens are exchanged, and leaving it strict keeps the guard narrow.
func endpointMismatches(source, target repo.RemoteSessionIssuer) []string {
	var mismatches []string

	if !issuerURLsCanonicallyEqual(source.Issuer, target.Issuer) {
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

// issuerURLsCanonicallyEqual reports whether two issuer identifiers name the
// same upstream authorization server, collapsing the trailing-slash and
// default-port spellings that parseCanonicalIssuerURL treats as equivalent.
//
// A value that does not parse as an issuer identifier is only ever equal to an
// identical string. Migration must not widen an identity comparison on input it
// could not understand, and rows predating validation can hold anything.
func issuerURLsCanonicallyEqual(a, b string) bool {
	if a == b {
		return true
	}

	canonicalA, err := parseCanonicalIssuerURL(a)
	if err != nil {
		return false
	}

	canonicalB, err := parseCanonicalIssuerURL(b)
	if err != nil {
		return false
	}

	return canonicalA.String() == canonicalB.String()
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

// resolveIssuerOrganizationID names the organization a tenant issuer belongs
// to. A legacy project-scoped issuer carries no organization_id of its own, so
// fall back to the owning project rather than treating it as untenanted.
func resolveIssuerOrganizationID(ctx context.Context, r *repo.Queries, issuer repo.RemoteSessionIssuer) (string, error) {
	if orgID := conv.FromPGTextOrEmpty[string](issuer.OrganizationID); orgID != "" {
		return orgID, nil
	}
	if !issuer.ProjectID.Valid {
		return "", fmt.Errorf("remote session issuer %s belongs to no tenant", issuer.ID)
	}

	orgID, err := r.GetProjectOrganizationID(ctx, issuer.ProjectID.UUID)
	if err != nil {
		return "", fmt.Errorf("resolve owning organization for remote session issuer %s: %w", issuer.ID, err)
	}
	return orgID, nil
}

// prepareIssuerMigrationResync reads the user session issuers a re-point will
// change the derivation of, and takes their derivation locks. First of two
// halves; extendIssuerMigrationResync is the second and is not optional.
//
// Both must happen before lockIssuersForMigration: every other writer takes the
// derivation lock before the remote-issuer one, so the reverse order deadlocks.
//
// Reading this early leaves a window — a binding that commits between the read
// and lockIssuersForMigration is not in the set, and the re-point would move
// its client without recomputing it. extendIssuerMigrationResync closes it.
func prepareIssuerMigrationResync(ctx context.Context, tx repo.DBTX, r *repo.Queries, sourceID uuid.UUID, organizationID string) ([]uuid.UUID, error) {
	affected, err := r.ListUserSessionIssuersBoundToRemoteIssuer(ctx, repo.ListUserSessionIssuersBoundToRemoteIssuerParams{
		RemoteSessionIssuerID: sourceID,
		OrganizationID:        organizationID,
	})
	if err != nil {
		return nil, fmt.Errorf("list user session issuers bound to source issuer: %w", err)
	}

	if err := LockUserSessionIssuersForRemoteIssuerDerivation(ctx, tx, affected); err != nil {
		return nil, err
	}

	return affected, nil
}

// errIssuerMigrationContended reports that a user session issuer appeared in
// the window and its derivation lock is held elsewhere, so this migration
// cannot cover it without waiting out of order. The handlers map it to a
// retryable conflict.
var errIssuerMigrationContended = errors.New("a client binding on the source issuer changed while the migration was starting")

// extendIssuerMigrationResync re-reads the affected set now that the source
// issuer's client-binding lock is held, unioned with what prepare already
// locked. The re-read is complete because every binding path takes that lock
// first, so nothing further can commit on the source while it is held.
//
// Newly-visible ids take the try-form: blocking would invert the global lock
// order and Postgres would call it a deadlock. Failing is cheap — this is an
// admin operation and the retry re-reads with the new binding included.
func extendIssuerMigrationResync(ctx context.Context, tx repo.DBTX, r *repo.Queries, sourceID uuid.UUID, organizationID string, locked []uuid.UUID) ([]uuid.UUID, error) {
	current, err := r.ListUserSessionIssuersBoundToRemoteIssuer(ctx, repo.ListUserSessionIssuersBoundToRemoteIssuerParams{
		RemoteSessionIssuerID: sourceID,
		OrganizationID:        organizationID,
	})
	if err != nil {
		return nil, fmt.Errorf("re-read user session issuers bound to source issuer: %w", err)
	}

	var added []uuid.UUID
	for _, id := range current {
		if !slices.Contains(locked, id) {
			added = append(added, id)
		}
	}

	if len(added) > 0 {
		acquired, err := TryLockUserSessionIssuersForRemoteIssuerDerivation(ctx, tx, added)
		if err != nil {
			return nil, err
		}
		if !acquired {
			return nil, errIssuerMigrationContended
		}
	}

	// Union, not the re-read alone: an id detached inside the window is no longer
	// in current, but its lock is held and recomputing it is a harmless no-op.
	union := slices.Concat(locked, added)
	slices.SortFunc(union, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	return slices.Compact(union), nil
}

// runIssuerMigration applies the guards every migration shares and then
// re-points the source's clients onto the target, returning how many moved. It
// is the whole of the operation apart from soft-deleting the source, which each
// surface does with its own scoped delete query.
//
// The two guards are the reason this is shared rather than duplicated. Endpoint
// parity is what keeps an already-authenticated session refreshing against the
// authorization server it was established with, and the binding-conflict check
// is the only thing enforcing the at-most-one-client-per-(user_session_issuer,
// remote_session_issuer) invariant, which no database constraint expresses. A
// surface that drifted on either would not merely behave differently, it would
// be less safe.
//
// Callers must already hold the advisory locks from lockIssuersForMigration and
// have re-read both issuers under a row lock, so that the rows validated here
// cannot change before the transaction commits. They must also have run
// prepareIssuerMigrationResync, which supplies affectedUserIssuerIDs and holds
// their derivation locks; organizationID bounds the resync. Its second half runs
// here rather than per surface, since it needs lockIssuersForMigration to have
// returned and a surface that forgot it would pass every non-racing test.
func runIssuerMigration(ctx context.Context, tx repo.DBTX, r *repo.Queries, logger *slog.Logger, source, target repo.RemoteSessionIssuer, organizationID string, affectedUserIssuerIDs []uuid.UUID) (int64, error) {
	affectedUserIssuerIDs, err := extendIssuerMigrationResync(ctx, tx, r, source.ID, organizationID, affectedUserIssuerIDs)
	switch {
	case errors.Is(err, errIssuerMigrationContended):
		return 0, oops.E(oops.CodeConflict, err, "a client binding on the source issuer changed while the migration was starting; retry").LogError(ctx, logger)
	case err != nil:
		return 0, oops.E(oops.CodeUnexpected, err, "extend issuer migration resync").LogError(ctx, logger)
	}

	preflight, err := buildMigratePreflight(ctx, r, source, target)
	if err != nil {
		return 0, oops.E(oops.CodeUnexpected, err, "build remote session issuer migrate preflight").LogError(ctx, logger)
	}

	if len(preflight.endpointMismatches) > 0 {
		return 0, oops.E(oops.CodeConflict, nil, "source and target issuers describe different authorization servers (%s differ); migration would break existing sessions", strings.Join(preflight.endpointMismatches, ", ")).LogError(ctx, logger)
	}

	if len(preflight.conflictingMcpServerNames) > 0 {
		return 0, oops.E(oops.CodeConflict, nil, "both issuers already have a client bound to the same MCP server (%s); detach one client per server and retry", strings.Join(preflight.conflictingMcpServerNames, ", ")).LogError(ctx, logger)
	}

	clientsMigrated, err := r.UpdateRemoteSessionClientsToRemoteSessionIssuer(ctx, repo.UpdateRemoteSessionClientsToRemoteSessionIssuerParams{
		TargetIssuerID: target.ID,
		SourceIssuerID: source.ID,
	})
	if err != nil {
		return 0, oops.E(oops.CodeUnexpected, err, "repoint remote session clients to target issuer").LogError(ctx, logger)
	}

	// The bindings did not move but what they resolve to did. Organization scope:
	// the re-pointed clients are shared across the source organization's projects.
	if err := ResyncMCPServerRemoteSessionIssuers(ctx, tx, OrganizationResyncScope(organizationID), affectedUserIssuerIDs); err != nil {
		return 0, oops.E(oops.CodeUnexpected, err, "resync mcp server remote session issuers").LogError(ctx, logger)
	}

	return clientsMigrated, nil
}
