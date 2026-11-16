package usersessions

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	orggen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/associationbackfill"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/selectionbackfill"
	networkingressrepo "github.com/speakeasy-api/gram/server/internal/networkingress/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

type userSessionIssuerScope int

const (
	userSessionIssuerScopeProject userSessionIssuerScope = iota
	userSessionIssuerScopeOrganization
)

func userSessionScopeOf(issuer repo.UserSessionIssuer) userSessionIssuerScope {
	if issuer.ProjectID.Valid {
		return userSessionIssuerScopeProject
	}
	return userSessionIssuerScopeOrganization
}

func validateUserSessionMigrationScope(source, target repo.UserSessionIssuer) error {
	sourceScope, targetScope := userSessionScopeOf(source), userSessionScopeOf(target)
	if targetScope < sourceScope {
		return errors.New("target issuer cannot be narrower than source issuer")
	}
	if sourceScope == userSessionIssuerScopeProject && targetScope == userSessionIssuerScopeProject && source.ProjectID.UUID != target.ProjectID.UUID {
		return errors.New("project-owned issuers must belong to the same project; move one issuer first or migrate onto an organization-owned issuer")
	}
	return nil
}

func parseUserSessionMigrationIDs(sourceRaw, targetRaw string) (uuid.UUID, uuid.UUID, error) {
	sourceID, err := uuid.Parse(sourceRaw)
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("invalid source issuer id: %w", err)
	}
	targetID, err := uuid.Parse(targetRaw)
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("invalid target issuer id: %w", err)
	}
	if sourceID == targetID {
		return uuid.Nil, uuid.Nil, errors.New("source and target issuer must differ")
	}
	return sourceID, targetID, nil
}

func loadUserSessionMigrationPair(ctx context.Context, q *repo.Queries, organizationID string, sourceID, targetID uuid.UUID, forUpdate bool) (repo.UserSessionIssuer, repo.UserSessionIssuer, error) {
	load := func(id uuid.UUID) (repo.UserSessionIssuer, error) {
		params := repo.GetOrganizationManagedUserSessionIssuerByIDParams{ID: id, OrganizationID: organizationID}
		if forUpdate {
			return q.GetOrganizationManagedUserSessionIssuerByIDForUpdate(ctx, repo.GetOrganizationManagedUserSessionIssuerByIDForUpdateParams(params))
		}
		return q.GetOrganizationManagedUserSessionIssuerByID(ctx, params)
	}

	source, err := load(sourceID)
	if err != nil {
		return repo.UserSessionIssuer{}, repo.UserSessionIssuer{}, fmt.Errorf("load source user session issuer: %w", err)
	}
	target, err := load(targetID)
	if err != nil {
		return repo.UserSessionIssuer{}, repo.UserSessionIssuer{}, fmt.Errorf("load target user session issuer: %w", err)
	}
	if source.Classification != "custom" || target.Classification != "custom" {
		return repo.UserSessionIssuer{}, repo.UserSessionIssuer{}, errors.New("project-default identity providers cannot be migrated")
	}
	if err := validateUserSessionMigrationScope(source, target); err != nil {
		return repo.UserSessionIssuer{}, repo.UserSessionIssuer{}, err
	}
	return source, target, nil
}

func lockUserSessionIssuersForMigration(ctx context.Context, q *repo.Queries, ids ...uuid.UUID) error {
	ordered := slices.Clone(ids)
	slices.SortFunc(ordered, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	for _, id := range ordered {
		if err := q.LockUserSessionIssuerForOwnerBinding(ctx, id); err != nil {
			return fmt.Errorf("lock user session issuer %s: %w", id, err)
		}
	}
	return nil
}

func lockUserSessionIssuerTargetProject(ctx context.Context, q *projectsrepo.Queries, projectID uuid.NullUUID, organizationID string) error {
	if !projectID.Valid {
		return nil
	}

	_, err := q.GetProjectByIDAndOrganizationIDForUpdate(ctx, projectsrepo.GetProjectByIDAndOrganizationIDForUpdateParams{
		ID:             projectID.UUID,
		OrganizationID: organizationID,
	})
	if err != nil {
		return fmt.Errorf("get target project for update: %w", err)
	}
	return nil
}

func projectIDValue(id uuid.NullUUID) uuid.UUID {
	if id.Valid {
		return id.UUID
	}
	return uuid.Nil
}

func nullableUUIDString(id uuid.NullUUID) string {
	if !id.Valid {
		return ""
	}
	return id.UUID.String()
}

func userSessionIssuerSlugConflict(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.ConstraintName == "user_session_issuers_project_slug_key"
}

// MoveIssuer changes issuer tenancy without changing its identity. Every row
// whose tenancy is owned by the issuer is normalized in the same transaction;
// project-scoped consumers remain in their own projects.
func (s *Service) MoveIssuer(ctx context.Context, payload *orggen.MoveIssuerPayload) (*types.UserSessionIssuer, error) {
	authCtx, err := s.requireOrganizationIssuerScope(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}
	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
	issuerID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid issuer id").LogError(ctx, logger)
	}

	targetProjectID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if payload.ProjectID != nil && strings.TrimSpace(*payload.ProjectID) != "" {
		parsed, parseErr := uuid.Parse(strings.TrimSpace(*payload.ProjectID))
		if parseErr != nil {
			return nil, oops.E(oops.CodeBadRequest, parseErr, "invalid project id").LogError(ctx, logger)
		}
		targetProjectID = conv.ToNullUUID(parsed)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	// Whether the organization has one organization-level issuer decides
	// whether its organization gateway fronts every server, so this change
	// re-decides associations in every project. Their writer scope precedes
	// every row lock.
	reconcileScope, err := upstreamclients.LockOrganizationScope(ctx, dbtx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock organization upstream client writer scope").LogError(ctx, logger)
	}
	q := repo.New(dbtx)

	if _, err := q.GetOrganizationManagedUserSessionIssuerByID(ctx, repo.GetOrganizationManagedUserSessionIssuerByIDParams{ID: issuerID, OrganizationID: authCtx.ActiveOrganizationID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "user session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "load user session issuer before lock").LogError(ctx, logger)
	}
	if err := lockUserSessionIssuersForMigration(ctx, q, issuerID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock user session issuer").LogError(ctx, logger)
	}
	existing, err := q.GetOrganizationManagedUserSessionIssuerByIDForUpdate(ctx, repo.GetOrganizationManagedUserSessionIssuerByIDForUpdateParams{ID: issuerID, OrganizationID: authCtx.ActiveOrganizationID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeConflict, err, "user session issuer changed while it was being locked; retry the move").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "lock user session issuer row").LogError(ctx, logger)
	}
	if existing.Classification != "custom" {
		return nil, oops.E(oops.CodeConflict, nil, "project-default identity providers cannot be moved").LogError(ctx, logger)
	}
	if err := lockUserSessionIssuerTargetProject(ctx, projectsrepo.New(dbtx), targetProjectID, authCtx.ActiveOrganizationID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeBadRequest, err, "project not found in organization").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "lock target project").LogError(ctx, logger)
	}
	if err := q.LockUserSessionIssuerClientsForMigration(ctx, issuerID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock user session issuer clients").LogError(ctx, logger)
	}
	owned, err := q.UserSessionIssuerIsPlatformOwned(ctx, repo.UserSessionIssuerIsPlatformOwnedParams{UserSessionIssuerID: conv.ToNullUUID(issuerID), OrganizationID: authCtx.ActiveOrganizationID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "check Platform MCP issuer ownership").LogError(ctx, logger)
	}
	if owned {
		return nil, oops.E(oops.CodeConflict, nil, "Platform MCP catalog-owned issuers cannot be moved").LogError(ctx, logger)
	}
	if targetProjectID.Valid {
		incompatible, err := q.CountUserSessionIssuerIncompatibleProjectReferences(ctx, repo.CountUserSessionIssuerIncompatibleProjectReferencesParams{
			UserSessionIssuerID: issuerID,
			OrganizationID:      authCtx.ActiveOrganizationID,
			TargetProjectID:     targetProjectID.UUID,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "count incompatible project references").LogError(ctx, logger)
		}
		if incompatible > 0 {
			return nil, oops.E(oops.CodeConflict, nil, "issuer has consumers outside the target project").LogError(ctx, logger)
		}
	}

	before := UserSessionIssuerView(existing)
	updated, err := q.SetOrganizationManagedUserSessionIssuerProject(ctx, repo.SetOrganizationManagedUserSessionIssuerProjectParams{ProjectID: targetProjectID, OrganizationID: authCtx.ActiveOrganizationID, ID: issuerID})
	if err != nil {
		if userSessionIssuerSlugConflict(err) {
			return nil, oops.E(oops.CodeConflict, err, "an issuer with this slug already exists in the target project; rename it first").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "move user session issuer").LogError(ctx, logger)
	}
	retier := []struct {
		name string
		fn   func() (int64, error)
	}{
		{name: "clients", fn: func() (int64, error) {
			return q.RetierUserSessionClients(ctx, repo.RetierUserSessionClientsParams{ProjectID: targetProjectID, OrganizationID: authCtx.ActiveOrganizationID, UserSessionIssuerID: issuerID})
		}},
		{name: "sessions", fn: func() (int64, error) {
			return q.RetierUserSessions(ctx, repo.RetierUserSessionsParams{ProjectID: targetProjectID, OrganizationID: authCtx.ActiveOrganizationID, UserSessionIssuerID: issuerID})
		}},
		{name: "consents", fn: func() (int64, error) {
			return q.RetierUserSessionConsents(ctx, repo.RetierUserSessionConsentsParams{ProjectID: targetProjectID, OrganizationID: authCtx.ActiveOrganizationID, UserSessionIssuerID: issuerID})
		}},
		{name: "CIMD clients", fn: func() (int64, error) {
			return q.RetierUserSessionIssuerCimdClients(ctx, repo.RetierUserSessionIssuerCimdClientsParams{ProjectID: targetProjectID, OrganizationID: authCtx.ActiveOrganizationID, UserSessionIssuerID: issuerID})
		}},
	}
	for _, operation := range retier {
		if _, err := operation.fn(); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "re-tier user session issuer %s", operation.name).LogError(ctx, logger)
		}
	}
	after := UserSessionIssuerView(updated)
	if err := s.audit.LogUserSessionIssuerUpdate(ctx, dbtx, audit.LogUserSessionIssuerUpdateEvent{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: projectIDValue(updated.ProjectID),
		Actor: urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), ActorDisplayName: authCtx.Email, ActorSlug: nil,
		UserSessionIssuerURN: urn.NewUserSessionIssuer(updated.ID), Slug: updated.Slug,
		UserSessionIssuerSnapshotBefore: before, UserSessionIssuerSnapshotAfter: after,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log user session issuer move").LogError(ctx, logger)
	}
	moved, err := q.GetOrganizationManagedUserSessionIssuerWithProjectByID(ctx, repo.GetOrganizationManagedUserSessionIssuerWithProjectByIDParams{ID: updated.ID, OrganizationID: authCtx.ActiveOrganizationID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read moved user session issuer").LogError(ctx, logger)
	}
	if err := upstreamclients.ReconcileOrganizationInLockedTransaction(ctx, dbtx, authCtx.ActiveOrganizationID, reconcileScope); err != nil {
		if errors.Is(err, upstreamclients.ErrScopeChanged) {
			return nil, err //nolint:wrapcheck // oops errors pass through.
		}
		return nil, oops.E(oops.CodeUnexpected, err, "reconcile organization upstream clients").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}
	return s.organizationIssuerView(ctx, moved.UserSessionIssuer, moved.ProjectName), nil
}

type userSessionMigrationPreflight struct {
	clientCount, sessionCount, consentCount, cimdClientCount, remoteSessionCount int32
	conflictingClientIDs                                                         []string
	principalBindingConflictCount, emaBindingConflictCount                       int32
	platformOwned                                                                bool
	warnings                                                                     []*orggen.UserSessionIssuerFieldMismatch
}

func (p userSessionMigrationPreflight) canMigrate() bool {
	return len(p.conflictingClientIDs) == 0 && p.principalBindingConflictCount == 0 && p.emaBindingConflictCount == 0 && !p.platformOwned
}

// userSessionMigrationWarnings lists the configuration differences an
// administrator must confirm before the target's values become authoritative.
// Deterministic side effects of every migration, such as issuer-bound access
// tokens being rejected until refreshed, are documented on the endpoint
// instead of reported here: a warning that never varies carries no decision.
//
// The authorization server settings are reported because repointed MCP servers
// then direct clients to the target's authorization server. issuer_url differs
// exactly when either issuer serves a shared authorization server, whose issuer
// identifier names that issuer; anything configured with the source's must be
// updated.
func (s *Service) userSessionMigrationWarnings(ctx context.Context, source, target repo.UserSessionIssuer) []*orggen.UserSessionIssuerFieldMismatch {
	sourceView, targetView := s.issuerView(ctx, source), s.issuerView(ctx, target)
	settings := []struct{ field, source, target string }{
		{field: "session_duration_hours", source: strconv.Itoa(sourceView.SessionDurationHours), target: strconv.Itoa(targetView.SessionDurationHours)},
		{field: "client_id_metadata_admission_mode", source: sourceView.ClientIDMetadataAdmissionMode, target: targetView.ClientIDMetadataAdmissionMode},
		{field: "authn_challenge_mode", source: source.AuthnChallengeMode, target: target.AuthnChallengeMode},
		{field: "trusted_remote_session_issuer_id", source: nullableUUIDString(source.TrustedRemoteSessionIssuerID), target: nullableUUIDString(target.TrustedRemoteSessionIssuerID)},
		{field: "trusted_remote_session_client_id", source: nullableUUIDString(source.TrustedRemoteSessionClientID), target: nullableUUIDString(target.TrustedRemoteSessionClientID)},
		{field: "authorization_server_mode", source: sourceView.AuthorizationServerMode, target: targetView.AuthorizationServerMode},
		{field: "use_authentication_host", source: strconv.FormatBool(sourceView.UseAuthenticationHost), target: strconv.FormatBool(targetView.UseAuthenticationHost)},
		{field: "issuer_url", source: conv.PtrValOr(sourceView.IssuerURL, ""), target: conv.PtrValOr(targetView.IssuerURL, "")},
	}
	warnings := make([]*orggen.UserSessionIssuerFieldMismatch, 0, len(settings))
	for _, setting := range settings {
		if setting.source != setting.target {
			warnings = append(warnings, &orggen.UserSessionIssuerFieldMismatch{Field: setting.field, SourceValue: setting.source, TargetValue: setting.target})
		}
	}
	return warnings
}

func (s *Service) buildUserSessionMigrationPreflight(ctx context.Context, q *repo.Queries, organizationID string, source, target repo.UserSessionIssuer) (userSessionMigrationPreflight, error) {
	clients, err := q.CountUserSessionIssuerClientsForMigration(ctx, repo.CountUserSessionIssuerClientsForMigrationParams{UserSessionIssuerID: source.ID, OrganizationID: organizationID})
	if err != nil {
		return userSessionMigrationPreflight{}, fmt.Errorf("count clients: %w", err)
	}
	sessions, err := q.CountUserSessionIssuerSessionsForMigration(ctx, repo.CountUserSessionIssuerSessionsForMigrationParams{UserSessionIssuerID: source.ID, OrganizationID: organizationID})
	if err != nil {
		return userSessionMigrationPreflight{}, fmt.Errorf("count sessions: %w", err)
	}
	consents, err := q.CountUserSessionIssuerConsentsForMigration(ctx, repo.CountUserSessionIssuerConsentsForMigrationParams{UserSessionIssuerID: source.ID, OrganizationID: organizationID})
	if err != nil {
		return userSessionMigrationPreflight{}, fmt.Errorf("count consents: %w", err)
	}
	cimdClients, err := q.CountUserSessionIssuerCimdClientsForMigration(ctx, repo.CountUserSessionIssuerCimdClientsForMigrationParams{UserSessionIssuerID: source.ID, OrganizationID: organizationID})
	if err != nil {
		return userSessionMigrationPreflight{}, fmt.Errorf("count CIMD clients: %w", err)
	}
	remoteSessions, err := q.CountUserSessionIssuerRemoteSessionsForMigration(ctx, repo.CountUserSessionIssuerRemoteSessionsForMigrationParams{UserSessionIssuerID: source.ID, OrganizationID: organizationID})
	if err != nil {
		return userSessionMigrationPreflight{}, fmt.Errorf("count remote sessions: %w", err)
	}
	conflicts, err := q.ListUserSessionIssuerClientIDConflicts(ctx, repo.ListUserSessionIssuerClientIDConflictsParams{TargetIssuerID: target.ID, SourceIssuerID: source.ID, OrganizationID: organizationID})
	if err != nil {
		return userSessionMigrationPreflight{}, fmt.Errorf("list client id conflicts: %w", err)
	}
	principalConflicts, err := q.CountUserSessionIssuerPrincipalBindingConflicts(ctx, repo.CountUserSessionIssuerPrincipalBindingConflictsParams{TargetIssuerID: target.ID, SourceIssuerID: source.ID, OrganizationID: organizationID})
	if err != nil {
		return userSessionMigrationPreflight{}, fmt.Errorf("count principal binding conflicts: %w", err)
	}
	emaConflicts, err := q.CountUserSessionIssuerEMABindingConflicts(ctx, repo.CountUserSessionIssuerEMABindingConflictsParams{TargetIssuerID: target.ID, SourceIssuerID: source.ID, OrganizationID: organizationID})
	if err != nil {
		return userSessionMigrationPreflight{}, fmt.Errorf("count EMA binding conflicts: %w", err)
	}
	sourceOwned, err := q.UserSessionIssuerIsPlatformOwned(ctx, repo.UserSessionIssuerIsPlatformOwnedParams{UserSessionIssuerID: conv.ToNullUUID(source.ID), OrganizationID: organizationID})
	if err != nil {
		return userSessionMigrationPreflight{}, fmt.Errorf("check source Platform MCP ownership: %w", err)
	}
	targetOwned, err := q.UserSessionIssuerIsPlatformOwned(ctx, repo.UserSessionIssuerIsPlatformOwnedParams{UserSessionIssuerID: conv.ToNullUUID(target.ID), OrganizationID: organizationID})
	if err != nil {
		return userSessionMigrationPreflight{}, fmt.Errorf("check target Platform MCP ownership: %w", err)
	}
	warnings := s.userSessionMigrationWarnings(ctx, source, target)
	return userSessionMigrationPreflight{
		clientCount: clients, sessionCount: sessions, consentCount: consents, cimdClientCount: cimdClients, remoteSessionCount: remoteSessions,
		conflictingClientIDs: conflicts, principalBindingConflictCount: principalConflicts, emaBindingConflictCount: emaConflicts,
		platformOwned: sourceOwned || targetOwned, warnings: warnings,
	}, nil
}

func userSessionMigrationPreflightView(run issuerMigrationRun) *orggen.OrganizationUserSessionIssuerMigratePreflight {
	preflight := run.preflight
	return &orggen.OrganizationUserSessionIssuerMigratePreflight{
		ClientCount: int(preflight.clientCount), SessionCount: int(preflight.sessionCount), ConsentCount: int(preflight.consentCount), CimdClientCount: int(preflight.cimdClientCount), RemoteSessionCount: int(preflight.remoteSessionCount),
		ConflictingClientIds: preflight.conflictingClientIDs, PrincipalBindingConflictCount: int(preflight.principalBindingConflictCount), EmaBindingConflictCount: int(preflight.emaBindingConflictCount), PlatformOwned: preflight.platformOwned,
		Warnings: preflight.warnings, WarningsFingerprint: run.fingerprint, CanMigrate: run.canMigrate(),
		ImpactPreviewed:           run.ran,
		ServerImpacts:             serverImpactViews(run.impacts),
		DroppedClientLinks:        migrationClientViews(run.dropped),
		UnscopedSessionCount:      int(run.reach.UnscopedSessionCount),
		NewlyReachableServerCount: int(run.reach.TargetServerCount),
	}
}

// GetIssuerMigratePreflight reports what consolidating the source onto the
// target would do. Without a hard blocker it runs the consolidation in a
// transaction it always rolls back, so the server impacts it reports are
// exactly what a consolidation committed now would do, including what
// reconciling the affected projects writes. It takes the consolidation's
// locks, bounded by a lock timeout, which is why it needs org:admin.
func (s *Service) GetIssuerMigratePreflight(ctx context.Context, payload *orggen.GetIssuerMigratePreflightPayload) (*orggen.OrganizationUserSessionIssuerMigratePreflight, error) {
	authCtx, err := s.requireOrganizationIssuerScope(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}
	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
	sourceID, targetID, err := parseUserSessionMigrationIDs(payload.SourceID, payload.TargetID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "%v", err).LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	if err := repo.New(dbtx).SetUserSessionIssuerMigrationPreviewLockTimeout(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "bound user session issuer migration preview locks").LogError(ctx, logger)
	}
	run, err := s.runIssuerMigration(ctx, dbtx, authCtx, logger, sourceID, targetID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && (pgErr.Code == pgerrcode.LockNotAvailable || pgErr.Code == pgerrcode.DeadlockDetected) {
			return nil, oops.E(oops.CodeConflict, err, "another change to these issuers or their servers is in progress; retry the preview").LogWarn(ctx, logger)
		}
		return nil, err
	}
	return userSessionMigrationPreflightView(run), nil
}

// mergedCimdClients counts the source rows folded into the target issuer's
// rows by mergeDuplicateCimdClients.
type mergedCimdClients struct {
	// clients is the number of source CIMD clients retired in favor of the
	// target's row for the same metadata document URL.
	clients int64

	// consents is the number of source consents repointed to a target client
	// or retired because the target already held the same consent.
	consents int64
}

// mergeDuplicateCimdClients folds each source client resolved from a Client
// ID Metadata Document into the target issuer's client for the same document
// URL. Both rows describe one client, so the target row survives: its consents
// win where the same subject approved the same upstream set on both issuers,
// and every other consent and session moves to it. Each pair's steps run in
// order, each seeing the previous step's writes.
func mergeDuplicateCimdClients(ctx context.Context, q *repo.Queries, organizationID string, sourceIssuerID, targetIssuerID uuid.UUID) (mergedCimdClients, error) {
	pairs, err := q.ListMergeableCimdClientPairs(ctx, repo.ListMergeableCimdClientPairsParams{TargetIssuerID: targetIssuerID, SourceIssuerID: sourceIssuerID, OrganizationID: organizationID})
	if err != nil {
		return mergedCimdClients{}, fmt.Errorf("list mergeable clients: %w", err)
	}

	merged := mergedCimdClients{clients: 0, consents: 0}
	for _, pair := range pairs {
		source, target := pair.SourceClientID, pair.TargetClientID

		if _, err := q.MoveDuplicateConsentResourcesToClient(ctx, repo.MoveDuplicateConsentResourcesToClientParams{SourceClientID: source, TargetClientID: target}); err != nil {
			return mergedCimdClients{}, fmt.Errorf("move consent resources of client %s: %w", source, err)
		}

		if _, err := q.RetireDuplicateConsentResources(ctx, repo.RetireDuplicateConsentResourcesParams{SourceClientID: source, TargetClientID: target}); err != nil {
			return mergedCimdClients{}, fmt.Errorf("retire duplicate consent resources of client %s: %w", source, err)
		}

		retired, err := q.RetireDuplicateConsents(ctx, repo.RetireDuplicateConsentsParams{SourceClientID: source, TargetClientID: target})
		if err != nil {
			return mergedCimdClients{}, fmt.Errorf("retire duplicate consents of client %s: %w", source, err)
		}

		repointed, err := q.RepointConsentsToClient(ctx, repo.RepointConsentsToClientParams{SourceClientID: source, TargetClientID: target})
		if err != nil {
			return mergedCimdClients{}, fmt.Errorf("repoint consents of client %s: %w", source, err)
		}

		if _, err := q.RepointSessionsToClient(ctx, repo.RepointSessionsToClientParams{SourceClientID: source, TargetClientID: target}); err != nil {
			return mergedCimdClients{}, fmt.Errorf("repoint sessions of client %s: %w", source, err)
		}

		clients, err := q.RetireMergedUserSessionClient(ctx, source)
		if err != nil {
			return mergedCimdClients{}, fmt.Errorf("retire client %s: %w", source, err)
		}

		merged.clients += clients
		merged.consents += retired + repointed
	}

	return merged, nil
}

// issuerMigrationScope is what lockIssuerMigrationAuth locked before any
// consolidation write.
type issuerMigrationScope struct {
	// serverIDs are every server whose credential reach the consolidation can
	// change: servers on either issuer, members of gateways on either, every
	// proxied server of the organization when either issuer is
	// organization-level, servers reached through the moved bindings, and
	// servers on the organization's only organization-level issuer that share
	// a client binding with either issuer.
	serverIDs []uuid.UUID

	// syncServers are the servers whose typed auth clients and credential
	// choices need maintenance.
	syncServers []uuid.UUID

	// projects are the projects whose writer scope is held, sorted. The
	// consolidation reconciles exactly these, so it never takes a writer
	// scope after its first write.
	projects []uuid.UUID

	// issuerIDs are the user session issuers whose servers are in serverIDs
	// by issuer: the source, the target, and the organization's only
	// organization-level issuer when its servers share a client with them.
	issuerIDs []uuid.UUID
}

// lockIssuerMigrationAuth prepares the complete credential reach before any
// issuer mutation. The shared preflight combines server registrations with
// issuer bindings that sole-consumer capture can associate later.
func lockIssuerMigrationAuth(ctx context.Context, dbtx pgx.Tx, organizationID string, source, target repo.UserSessionIssuer) (issuerMigrationScope, error) {
	q := repo.New(dbtx)
	issuerIDs := []uuid.UUID{source.ID, target.ID}
	servers, err := q.ListMCPServersForUserSessionIssuerMigration(ctx, repo.ListMCPServersForUserSessionIssuerMigrationParams{
		IssuerIds: issuerIDs, SourceIssuerID: source.ID, OrganizationID: organizationID,
	})
	if err != nil {
		return issuerMigrationScope{}, fmt.Errorf("discover issuer migration writer scope: %w", err)
	}
	// Non-nil is intentional: empty means no maintenance, while nil would
	// widen existing target credentials to their full issuer reach downstream.
	scope := issuerMigrationScope{serverIDs: nil, syncServers: make([]uuid.UUID, 0), projects: nil, issuerIDs: []uuid.UUID{source.ID, target.ID}}
	for _, server := range servers {
		scope.projects = append(scope.projects, server.ProjectID)
		scope.serverIDs = append(scope.serverIDs, server.ID)
		if server.NeedsSync {
			scope.syncServers = append(scope.syncServers, server.ID)
		}
	}
	// Servers on the organization's only organization-level issuer can lose a
	// client they share with either issuer once a consolidated server is
	// associated with it, so they are previewed and reconciled too.
	claiming, err := q.ListMCPServersOnClaimingOrganizationIssuerForMigration(ctx, repo.ListMCPServersOnClaimingOrganizationIssuerForMigrationParams{
		OrganizationID: organizationID, SourceIssuerID: source.ID, TargetIssuerID: target.ID,
	})
	if err != nil {
		return issuerMigrationScope{}, fmt.Errorf("discover servers on the organization issuer: %w", err)
	}
	for _, server := range claiming {
		if !slices.Contains(scope.serverIDs, server.ID) {
			scope.serverIDs = append(scope.serverIDs, server.ID)
			scope.projects = append(scope.projects, server.ProjectID)
		}
		// A client it newly loses can leave its typed offers and credential
		// choices stale, so it is maintained like the moved servers.
		if !slices.Contains(scope.syncServers, server.ID) {
			scope.syncServers = append(scope.syncServers, server.ID)
		}
		if !slices.Contains(scope.issuerIDs, server.UserSessionIssuerID) {
			scope.issuerIDs = append(scope.issuerIDs, server.UserSessionIssuerID)
		}
	}
	for _, issuer := range []repo.UserSessionIssuer{source, target} {
		if issuer.ProjectID.Valid {
			scope.projects = append(scope.projects, issuer.ProjectID.UUID)
		}
	}
	slices.SortFunc(scope.projects, compareUUIDs)
	scope.projects = slices.Compact(scope.projects)
	if err := authclientbackfill.LockEMAWriterScope(ctx, dbtx, organizationID, scope.projects); err != nil {
		return issuerMigrationScope{}, fmt.Errorf("lock issuer migration writer scope: %w", err)
	}
	registrations, err := q.ListBoundOAuthRegistrationsForUserSessionIssuerMigration(ctx, repo.ListBoundOAuthRegistrationsForUserSessionIssuerMigrationParams{
		IssuerIds: issuerIDs, OrganizationID: organizationID,
	})
	if err != nil {
		return issuerMigrationScope{}, fmt.Errorf("discover issuer migration registration scope: %w", err)
	}
	if err := authclientbackfill.PreflightServerAssociationsWithRegistrations(ctx, dbtx, organizationID, scope.serverIDs, registrations); err != nil {
		return issuerMigrationScope{}, fmt.Errorf("preflight issuer migration auth clients: %w", err)
	}
	return scope, nil
}

// reconcileIssuerMigrationAuth preserves typed state and credential choices in
// the consolidation transaction, independent of the enforcement feature flag.
func reconcileIssuerMigrationAuth(ctx context.Context, dbtx pgx.Tx, organizationID string, issuerID uuid.UUID, serverIDs, movedCredentialIDs []uuid.UUID) (int64, error) {
	if err := authclientbackfill.SyncServerAssociations(ctx, dbtx, organizationID, serverIDs...); err != nil {
		return 0, fmt.Errorf("sync issuer auth clients: %w", err)
	}
	skipped, err := selectionbackfill.ReconcileIssuerScopeInLockedTransaction(ctx, dbtx, organizationID, issuerID, serverIDs, movedCredentialIDs)
	if err != nil {
		return 0, fmt.Errorf("reconcile issuer credential choices: %w", err)
	}
	return skipped, nil
}

// recordIssuerMigrationChoices records what each live credential uses today,
// before the source's bindings join the target's. A server both issuers bound a
// registration for has one eligible client per credential until then and two
// afterwards, which would leave every credential without a choice to reconnect.
// Source lineages record their whole reach: they move, and what they reach on
// the target is decided after the move.
func recordIssuerMigrationChoices(ctx context.Context, dbtx pgx.Tx, organizationID string, sourceID, targetID uuid.UUID, serverIDs []uuid.UUID) error {
	if err := selectionbackfill.RecordIssuerScopeInLockedTransaction(ctx, dbtx, organizationID, sourceID, nil, nil); err != nil {
		return fmt.Errorf("record source credential choices: %w", err)
	}
	if err := selectionbackfill.RecordIssuerScopeInLockedTransaction(ctx, dbtx, organizationID, targetID, serverIDs, nil); err != nil {
		return fmt.Errorf("record target credential choices: %w", err)
	}
	return nil
}

// issuerMigrationRun is what running a consolidation in a transaction did.
// MigrateIssuer commits it; the preflight rolls it back and reports it.
type issuerMigrationRun struct {
	// source is the issuer consolidated away, as loaded under lock.
	source repo.UserSessionIssuer

	// target is the surviving issuer, as loaded under lock.
	target repo.UserSessionIssuer

	// preflight holds the counts, hard blockers and setting differences read
	// under lock before any write.
	preflight userSessionMigrationPreflight

	// ran reports that the preflight had no hard blocker, so the
	// consolidation ran and everything below it was computed.
	ran bool

	// reach is how far the source's sign-ins reach once they move, read
	// before any write.
	reach repo.GetUserSessionIssuerMigrationSignInReachRow

	// pins are the associations written before the move.
	pins []associationbackfill.Pin

	// impacts are the servers whose clients changed or that were pinned.
	impacts []serverImpact

	// dropped are the source's bindings not carried to the target, ordered
	// by client id.
	dropped []migrationClient

	// fingerprint covers the setting differences, impacts and dropped links.
	fingerprint string

	// deletedSource is the source as soft-deleted.
	deletedSource repo.UserSessionIssuer

	clientsMigrated, sessionsMigrated, consentsMigrated, cimdClientsMigrated, remoteSessionsMigrated int64

	// skippedMappings counts credential choices left alone because they
	// changed meanwhile.
	skippedMappings int64
}

// canMigrate reports whether the consolidation may commit: no hard blocker,
// and no server impact of blocker severity.
func (r issuerMigrationRun) canMigrate() bool {
	return r.preflight.canMigrate() && !hasBlocker(r.impacts)
}

// runIssuerMigration consolidates the source onto the target in dbtx, which
// the caller commits or rolls back. When the preflight it reads under lock
// reports a hard blocker it stops before any write and returns with ran false.
//
// The order matters:
//
//  1. Every lock, including the writer scope of every project it reconciles,
//     is taken before the first write.
//  2. Each affected server's clients are read, then the associations its
//     configuration proves are pinned, before any binding moves.
//  3. Only the source bindings something still signs in through are copied.
//  4. The source is retired before the affected projects are reconciled, so
//     the reconcile sees the organization's issuers as they will be after the
//     commit, and the reconcile skips the gateway capture.
//  5. Each affected server's clients are read again and compared.
func (s *Service) runIssuerMigration(ctx context.Context, dbtx pgx.Tx, authCtx *contextvalues.AuthContext, logger *slog.Logger, sourceID, targetID uuid.UUID) (issuerMigrationRun, error) {
	var run issuerMigrationRun
	organizationID := authCtx.ActiveOrganizationID

	// An inherited issuer can gain its first server in another project. Protect
	// discovery before any ingress/issuer locks; never upgrade a shared gate.
	if err := authclientbackfill.New(dbtx).LockEMAGatewayReachWriter(ctx, organizationID); err != nil {
		return run, oops.E(oops.CodeUnexpected, err, "lock issuer migration reach").LogError(ctx, logger)
	}
	q := repo.New(dbtx)
	source, target, err := loadUserSessionMigrationPair(ctx, q, organizationID, sourceID, targetID, false)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return run, oops.E(oops.CodeNotFound, err, "source or target user session issuer not found").LogError(ctx, logger)
		}
		return run, oops.E(oops.CodeBadRequest, err, "%v", err).LogError(ctx, logger)
	}
	scope, err := lockIssuerMigrationAuth(ctx, dbtx, organizationID, source, target)
	if err != nil {
		return run, oops.E(oops.CodeUnexpected, err, "lock issuer migration auth scope").LogError(ctx, logger)
	}
	// Hosted toolset saves take this lock before issuer and toolset locks.
	if err := networkingressrepo.New(dbtx).AcquireNetworkIngressOrganizationLock(ctx, organizationID); err != nil {
		return run, oops.E(oops.CodeUnexpected, err, "lock network ingress lifecycle").LogError(ctx, logger)
	}
	if err := lockUserSessionIssuersForMigration(ctx, q, source.ID, target.ID); err != nil {
		return run, oops.E(oops.CodeUnexpected, err, "lock issuers for migration").LogError(ctx, logger)
	}
	source, target, err = loadUserSessionMigrationPair(ctx, q, organizationID, sourceID, targetID, true)
	if err != nil {
		return run, oops.E(oops.CodeConflict, err, "issuer scope changed while migration was being locked; retry").LogError(ctx, logger)
	}
	if err := lockUserSessionIssuerTargetProject(ctx, projectsrepo.New(dbtx), target.ProjectID, organizationID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return run, oops.E(oops.CodeConflict, err, "target project changed while migration was being locked; retry").LogError(ctx, logger)
		}
		return run, oops.E(oops.CodeUnexpected, err, "lock target project").LogError(ctx, logger)
	}
	for _, issuer := range []repo.UserSessionIssuer{source, target} {
		if issuer.ProjectID.Valid && (!issuer.OrganizationID.Valid || issuer.OrganizationID.String != organizationID) {
			if _, err := q.NormalizeLegacyProjectUserSessionIssuerOrganization(ctx, repo.NormalizeLegacyProjectUserSessionIssuerOrganizationParams{ID: issuer.ID, OrganizationID: organizationID}); err != nil {
				return run, oops.E(oops.CodeUnexpected, err, "normalize project user session issuer organization").LogError(ctx, logger)
			}
		}
	}
	source, target, err = loadUserSessionMigrationPair(ctx, q, organizationID, sourceID, targetID, true)
	if err != nil {
		return run, oops.E(oops.CodeConflict, err, "issuer scope changed while migration was normalized; retry").LogError(ctx, logger)
	}
	if err := q.LockUserSessionIssuerClientsForMigration(ctx, source.ID); err != nil {
		return run, oops.E(oops.CodeUnexpected, err, "lock source user session clients").LogError(ctx, logger)
	}
	// Merged CIMD clients take their consents onto the target's rows, so a
	// consent recorded on a target client mid-migration must wait.
	if err := q.LockUserSessionIssuerClientsForMigration(ctx, target.ID); err != nil {
		return run, oops.E(oops.CodeUnexpected, err, "lock target user session clients").LogError(ctx, logger)
	}
	preflight, err := s.buildUserSessionMigrationPreflight(ctx, q, organizationID, source, target)
	if err != nil {
		return run, oops.E(oops.CodeUnexpected, err, "build user session issuer migrate preflight").LogError(ctx, logger)
	}
	run.source, run.target, run.preflight = source, target, preflight
	run.fingerprint = userSessionMigrationFingerprint(source.ID, target.ID, preflight.warnings, nil, nil)
	if !preflight.canMigrate() {
		return run, nil
	}
	run.ran = true

	run.reach, err = q.GetUserSessionIssuerMigrationSignInReach(ctx, repo.GetUserSessionIssuerMigrationSignInReachParams{
		SourceIssuerID: source.ID, TargetIssuerID: target.ID, OrganizationID: organizationID,
	})
	if err != nil {
		return run, oops.E(oops.CodeUnexpected, err, "read user session issuer migration sign-in reach").LogError(ctx, logger)
	}
	before, err := snapshotServerClients(ctx, dbtx, organizationID, scope.serverIDs)
	if err != nil {
		return run, oops.E(oops.CodeUnexpected, err, "read MCP server upstream clients before migration").LogError(ctx, logger)
	}

	// Record the clients each server on either issuer provably uses while
	// its own issuer still says so; after the move the merged bindings no
	// longer prove which server a client was meant for.
	run.pins, err = associationbackfill.PinServers(ctx, dbtx, organizationID, before.serversOn(source.ID, target.ID), scope.issuerIDs, []uuid.UUID{source.ID})
	if err != nil {
		return run, oops.E(oops.CodeUnexpected, err, "pin MCP server upstream clients").LogError(ctx, logger)
	}
	if len(run.pins) > 0 {
		pinnedServers := make([]uuid.UUID, 0, len(run.pins))
		for _, pin := range run.pins {
			pinnedServers = append(pinnedServers, pin.MCPServerID)
		}
		if err := authclientbackfill.SyncServerAssociations(ctx, dbtx, organizationID, pinnedServers...); err != nil {
			return run, oops.E(oops.CodeUnexpected, err, "record auth clients of pinned MCP server upstream clients").LogError(ctx, logger)
		}
	}

	if err := recordIssuerMigrationChoices(ctx, dbtx, organizationID, source.ID, target.ID, scope.syncServers); err != nil {
		return run, oops.E(oops.CodeUnexpected, err, "record issuer migration credential choices").LogError(ctx, logger)
	}

	merged, err := mergeDuplicateCimdClients(ctx, q, organizationID, source.ID, target.ID)
	if err != nil {
		return run, oops.E(oops.CodeUnexpected, err, "merge duplicate CIMD user session clients").LogError(ctx, logger)
	}

	targetProjectID := target.ProjectID
	run.consentsMigrated, err = q.UpdateUserSessionConsentsToIssuerScope(ctx, repo.UpdateUserSessionConsentsToIssuerScopeParams{TargetProjectID: targetProjectID, TargetIssuerID: target.ID, OrganizationID: organizationID, SourceIssuerID: source.ID})
	if err != nil {
		return run, oops.E(oops.CodeUnexpected, err, "normalize migrated user session consents").LogError(ctx, logger)
	}
	run.clientsMigrated, err = q.UpdateUserSessionClientsToIssuer(ctx, repo.UpdateUserSessionClientsToIssuerParams{TargetIssuerID: target.ID, TargetProjectID: targetProjectID, OrganizationID: organizationID, SourceIssuerID: source.ID})
	if err != nil {
		return run, oops.E(oops.CodeUnexpected, err, "migrate user session clients").LogError(ctx, logger)
	}
	movedCredentialIDs, err := q.UpdateUserSessionsToIssuer(ctx, repo.UpdateUserSessionsToIssuerParams{TargetIssuerID: target.ID, TargetProjectID: targetProjectID, OrganizationID: organizationID, SourceIssuerID: source.ID})
	if err != nil {
		return run, oops.E(oops.CodeUnexpected, err, "migrate user sessions").LogError(ctx, logger)
	}
	run.sessionsMigrated = int64(len(movedCredentialIDs))
	// Merged clients and their consents now live on the target's rows, so they
	// count as migrated alongside the rows that moved as they were.
	run.clientsMigrated += merged.clients
	run.consentsMigrated += merged.consents
	if _, err := q.MergeDuplicateUserSessionIssuerCimdClients(ctx, repo.MergeDuplicateUserSessionIssuerCimdClientsParams{SourceIssuerID: source.ID, TargetIssuerID: target.ID, OrganizationID: organizationID}); err != nil {
		return run, oops.E(oops.CodeUnexpected, err, "merge duplicate CIMD clients").LogError(ctx, logger)
	}
	run.cimdClientsMigrated, err = q.UpdateUserSessionIssuerCimdClientsToIssuer(ctx, repo.UpdateUserSessionIssuerCimdClientsToIssuerParams{TargetIssuerID: target.ID, TargetProjectID: targetProjectID, OrganizationID: organizationID, SourceIssuerID: source.ID})
	if err != nil {
		return run, oops.E(oops.CodeUnexpected, err, "migrate CIMD clients").LogError(ctx, logger)
	}
	// Read before the source's bindings join the target's: only these were
	// configured for sign-in through the target.
	targetRegistrations, err := q.ListBoundOAuthRegistrationsForUserSessionIssuerMigration(ctx, repo.ListBoundOAuthRegistrationsForUserSessionIssuerMigrationParams{
		IssuerIds: []uuid.UUID{target.ID}, OrganizationID: organizationID,
	})
	if err != nil {
		return run, oops.E(oops.CodeUnexpected, err, "list target remote-session client links").LogError(ctx, logger)
	}
	// Carry only the bindings something still signs in through. The rest
	// leave with the source; users' grants for them still move below, so
	// binding such a client to the target later makes them usable again. A
	// deleted client's binding is never carried and changes nothing, so it is
	// not reported either.
	links, err := q.ListSourceClientLinksForUserSessionIssuerMigration(ctx, repo.ListSourceClientLinksForUserSessionIssuerMigrationParams{
		SourceIssuerID: source.ID, TargetIssuerID: target.ID, RequiredClientIds: before.clientsOfServersOn(source.ID), OrganizationID: organizationID,
	})
	if err != nil {
		return run, oops.E(oops.CodeUnexpected, err, "classify source remote-session client links").LogError(ctx, logger)
	}
	carried := make([]uuid.UUID, 0, len(links))
	for _, link := range links {
		switch {
		case link.Needed:
			carried = append(carried, link.RemoteSessionClientID)
		case !link.AlreadyOnTarget && !link.ClientDeleted:
			run.dropped = append(run.dropped, migrationClient{
				id: link.RemoteSessionClientID, clientID: link.ClientID,
				providerID: link.ProviderID.UUID, providerIssuer: link.ProviderIssuer.String,
			})
		}
	}
	if _, err := q.InsertTargetRemoteSessionClientIssuerLinks(ctx, repo.InsertTargetRemoteSessionClientIssuerLinksParams{TargetIssuerID: target.ID, SourceIssuerID: source.ID, RemoteSessionClientIds: carried, OrganizationID: organizationID}); err != nil {
		return run, oops.E(oops.CodeUnexpected, err, "create target remote-session client links").LogError(ctx, logger)
	}
	run.remoteSessionsMigrated, err = q.UpdateRemoteSessionsToUserSessionIssuer(ctx, repo.UpdateRemoteSessionsToUserSessionIssuerParams{TargetIssuerID: target.ID, SourceIssuerID: source.ID, OrganizationID: organizationID})
	if err != nil {
		return run, oops.E(oops.CodeUnexpected, err, "migrate remote-session provenance").LogError(ctx, logger)
	}
	if _, err := q.DeleteSourceRemoteSessionClientIssuerLinks(ctx, repo.DeleteSourceRemoteSessionClientIssuerLinksParams{SourceIssuerID: source.ID, TargetIssuerID: target.ID, OrganizationID: organizationID}); err != nil {
		return run, oops.E(oops.CodeUnexpected, err, "remove source remote-session client links").LogError(ctx, logger)
	}
	if _, err := q.UpdateRemoteSessionEMABindingsToUserSessionIssuer(ctx, repo.UpdateRemoteSessionEMABindingsToUserSessionIssuerParams{TargetIssuerID: target.ID, SourceIssuerID: source.ID, OrganizationID: organizationID}); err != nil {
		return run, oops.E(oops.CodeUnexpected, err, "migrate EMA bindings").LogError(ctx, logger)
	}
	mcpServerProjectIDs, err := q.ListMCPServerProjectIDsForUserSessionIssuerMigration(ctx, repo.ListMCPServerProjectIDsForUserSessionIssuerMigrationParams{
		SourceIssuerID: conv.ToNullUUID(source.ID),
		TargetIssuerID: conv.ToNullUUID(target.ID),
		OrganizationID: organizationID,
	})
	if err != nil {
		return run, oops.E(oops.CodeUnexpected, err, "list MCP server projects for issuer migration").LogError(ctx, logger)
	}
	movedServerProjects := map[uuid.UUID]bool{}
	ownerUpdates := []struct {
		name string
		fn   func() (int64, error)
	}{
		{name: "MCP servers", fn: func() (int64, error) {
			moved, err := q.UpdateMCPServersToUserSessionIssuer(ctx, repo.UpdateMCPServersToUserSessionIssuerParams{TargetIssuerID: conv.ToNullUUID(target.ID), SourceIssuerID: conv.ToNullUUID(source.ID), OrganizationID: organizationID})
			if err != nil {
				return 0, fmt.Errorf("move MCP servers: %w", err)
			}
			if err := auditHostedIssuerMoves(ctx, dbtx, s.audit, authCtx, moved, source.ID); err != nil {
				return 0, err
			}
			for _, server := range moved {
				movedServerProjects[server.ProjectID] = true
			}
			return int64(len(moved)), nil
		}},
		{name: "meta MCP servers", fn: func() (int64, error) {
			return q.UpdateMetaMCPServersToUserSessionIssuer(ctx, repo.UpdateMetaMCPServersToUserSessionIssuerParams{TargetIssuerID: conv.ToNullUUID(target.ID), SourceIssuerID: conv.ToNullUUID(source.ID), OrganizationID: organizationID})
		}},
		{name: "Platform MCP registrations", fn: func() (int64, error) {
			return q.UpdatePlatformMCPRegistrationsToUserSessionIssuer(ctx, repo.UpdatePlatformMCPRegistrationsToUserSessionIssuerParams{TargetIssuerID: conv.ToNullUUID(target.ID), SourceIssuerID: conv.ToNullUUID(source.ID), OrganizationID: organizationID})
		}},
	}
	for _, operation := range ownerUpdates {
		if _, err := operation.fn(); err != nil {
			return run, oops.E(oops.CodeUnexpected, err, "migrate user session issuer %s", operation.name).LogError(ctx, logger)
		}
	}
	for _, projectID := range mcpServerProjectIDs {
		// The migration moves the servers onto the target, which configures
		// their sign-in there, so a moved server that is the target's only
		// consumer offers the clients the target bound. A server already on
		// the target was not configured by this move, and a binding that only
		// the source held was configured for neither.
		if movedServerProjects[projectID] {
			if err := upstreamclients.CaptureSoleConsumers(ctx, dbtx, organizationID, projectID, []uuid.UUID{target.ID}, slices.Concat([]uuid.UUID{}, targetRegistrations)); err != nil {
				return run, oops.E(oops.CodeUnexpected, err, "capture MCP server upstream clients").LogError(ctx, logger)
			}
		}
	}
	// Retire the source before reconciling: prune and the organization
	// gateway decide from how many organization-level issuers are live, which
	// the commit changes when the source is one of them.
	run.deletedSource, err = q.SoftDeleteMigratedUserSessionIssuer(ctx, repo.SoftDeleteMigratedUserSessionIssuerParams{SourceIssuerID: source.ID, TargetIssuerID: target.ID, OrganizationID: organizationID})
	if err != nil {
		return run, oops.E(oops.CodeUnexpected, err, "soft-delete migrated user session issuer").LogError(ctx, logger)
	}
	// Reconcile every project whose writer scope is held, not only those with
	// a server on either issuer: a gateway member's project, and every
	// project when an organization-level issuer is involved, can change too.
	// Each project holding a server on either issuer is among them.
	for _, projectID := range scope.projects {
		if err := remotesessions.ResyncMCPServerRemoteSessionIssuersForConsolidation(ctx, dbtx, organizationID, projectID, []uuid.UUID{source.ID, target.ID}); err != nil {
			return run, oops.E(oops.CodeUnexpected, err, "resync MCP server remote session issuers").LogError(ctx, logger)
		}
	}
	run.skippedMappings, err = reconcileIssuerMigrationAuth(ctx, dbtx, organizationID, target.ID, scope.syncServers, movedCredentialIDs)
	if err != nil {
		return run, oops.E(oops.CodeUnexpected, err, "reconcile issuer migration auth").LogError(ctx, logger)
	}

	after, err := snapshotServerClients(ctx, dbtx, organizationID, scope.serverIDs)
	if err != nil {
		return run, oops.E(oops.CodeUnexpected, err, "read MCP server upstream clients after migration").LogError(ctx, logger)
	}
	run.impacts, err = classifyServerImpacts(ctx, dbtx, organizationID, before, after, run.pins)
	if err != nil {
		return run, oops.E(oops.CodeUnexpected, err, "classify MCP server upstream client changes").LogError(ctx, logger)
	}
	run.fingerprint = userSessionMigrationFingerprint(source.ID, target.ID, preflight.warnings, run.impacts, run.dropped)
	return run, nil
}

func (s *Service) MigrateIssuer(ctx context.Context, payload *orggen.MigrateIssuerPayload) (*orggen.MigrateOrganizationUserSessionIssuerResult, error) {
	authCtx, err := s.requireOrganizationIssuerScope(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}
	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
	sourceID, targetID, err := parseUserSessionMigrationIDs(payload.SourceID, payload.TargetID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "%v", err).LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	run, err := s.runIssuerMigration(ctx, dbtx, authCtx, logger, sourceID, targetID)
	if err != nil {
		return nil, err
	}
	if !run.ran || !run.canMigrate() {
		return nil, oops.E(oops.CodeConflict, nil, "user session issuer migration has unresolved blockers; refresh the preflight").LogError(ctx, logger)
	}
	if run.fingerprint != "" && (payload.ConfirmedWarningsFingerprint == nil || *payload.ConfirmedWarningsFingerprint != run.fingerprint) {
		return nil, oops.E(oops.CodeConflict, nil, "user session issuer migration warnings have not been confirmed or changed; refresh the preflight and confirm its exact warnings fingerprint").LogError(ctx, logger)
	}

	source, target := run.source, run.target
	dropped := make([]uuid.UUID, 0, len(run.dropped))
	for _, client := range run.dropped {
		dropped = append(dropped, client.id)
	}
	targetView := UserSessionIssuerView(target)
	if err := s.audit.LogUserSessionIssuerMigrate(ctx, dbtx, audit.LogUserSessionIssuerMigrateEvent{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: projectIDValue(run.deletedSource.ProjectID),
		Actor: urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), ActorDisplayName: authCtx.Email, ActorSlug: nil,
		SourceUserSessionIssuerURN: urn.NewUserSessionIssuer(source.ID), SourceSlug: source.Slug,
		TargetUserSessionIssuerURN: urn.NewUserSessionIssuer(target.ID), TargetSlug: target.Slug,
		ClientsMigrated: run.clientsMigrated, SessionsMigrated: run.sessionsMigrated, ConsentsMigrated: run.consentsMigrated, CimdClientsMigrated: run.cimdClientsMigrated, RemoteSessionsMigrated: run.remoteSessionsMigrated,
		PinnedAssociations: auditPins(run.pins), DroppedClientLinks: dropped, ServerImpacts: auditServerImpacts(run.impacts),
		UserSessionIssuerSnapshotBefore: UserSessionIssuerView(source), UserSessionIssuerSnapshotAfter: targetView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log user session issuer migration").LogError(ctx, logger)
	}
	surviving, err := repo.New(dbtx).GetOrganizationManagedUserSessionIssuerWithProjectByID(ctx, repo.GetOrganizationManagedUserSessionIssuerWithProjectByIDParams{ID: target.ID, OrganizationID: authCtx.ActiveOrganizationID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read surviving user session issuer").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}
	if run.skippedMappings > 0 {
		logger.DebugContext(ctx, "issuer consolidation skipped changed credential choices",
			attr.SlogUserSessionIssuerID(target.ID.String()), attr.SlogAuthClientBackfillSkipped(run.skippedMappings))
	}
	return &orggen.MigrateOrganizationUserSessionIssuerResult{
		Issuer: s.organizationIssuerView(ctx, surviving.UserSessionIssuer, surviving.ProjectName), ClientsMigrated: int(run.clientsMigrated), SessionsMigrated: int(run.sessionsMigrated), ConsentsMigrated: int(run.consentsMigrated), CimdClientsMigrated: int(run.cimdClientsMigrated), RemoteSessionsMigrated: int(run.remoteSessionsMigrated), SourceDeleted: true,
		ServerImpacts: serverImpactViews(run.impacts), DroppedClientLinks: migrationClientViews(run.dropped),
	}, nil
}

// auditHostedIssuerMoves records each hosted server the bulk issuer move changed as its own update.
func auditHostedIssuerMoves(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, authCtx *contextvalues.AuthContext, moved []repo.UpdateMCPServersToUserSessionIssuerRow, sourceID uuid.UUID) error {
	actor := hostedmcp.Actor{UserID: authCtx.UserID, Email: authCtx.Email, System: ""}
	for _, row := range moved {
		if !row.Canonical {
			continue
		}
		after, err := mcpserversrepo.New(tx).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: row.ID, ProjectID: row.ProjectID})
		if err != nil {
			return fmt.Errorf("load moved hosted MCP server: %w", err)
		}
		before := after
		before.UserSessionIssuerID = conv.ToNullUUID(sourceID)
		before.UpdatedAt = row.PreviousUpdatedAt
		if err := hostedmcp.LogUpdate(ctx, tx, auditLogger, actor, authCtx.ActiveOrganizationID, before, after); err != nil {
			return fmt.Errorf("audit hosted MCP issuer move: %w", err)
		}
	}
	return nil
}
