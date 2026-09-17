package usersessions

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	orggen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
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
	q := repo.New(dbtx)

	if targetProjectID.Valid {
		if _, err := projectsrepo.New(dbtx).GetProjectByIDAndOrganizationID(ctx, projectsrepo.GetProjectByIDAndOrganizationIDParams{ID: targetProjectID.UUID, OrganizationID: authCtx.ActiveOrganizationID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeBadRequest, err, "project not found in organization").LogError(ctx, logger)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "validate target project").LogError(ctx, logger)
		}
	}

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
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}
	return after, nil
}

type userSessionMigrationPreflight struct {
	clientCount, sessionCount, consentCount, cimdClientCount, remoteSessionCount int32
	conflictingClientIDs                                                         []string
	principalBindingConflictCount, emaBindingConflictCount                       int32
	platformOwned                                                                bool
	warnings                                                                     []*orggen.UserSessionIssuerFieldMismatch
	warningsFingerprint                                                          string
}

func (p userSessionMigrationPreflight) canMigrate() bool {
	return len(p.conflictingClientIDs) == 0 && p.principalBindingConflictCount == 0 && p.emaBindingConflictCount == 0 && !p.platformOwned
}

func userSessionMigrationWarnings(source, target repo.UserSessionIssuer) []*orggen.UserSessionIssuerFieldMismatch {
	sourceView, targetView := UserSessionIssuerView(source), UserSessionIssuerView(target)
	settings := []struct{ field, source, target string }{
		{field: "session_duration_hours", source: strconv.Itoa(sourceView.SessionDurationHours), target: strconv.Itoa(targetView.SessionDurationHours)},
		{field: "client_id_metadata_admission_mode", source: sourceView.ClientIDMetadataAdmissionMode, target: targetView.ClientIDMetadataAdmissionMode},
		{field: "authn_challenge_mode", source: source.AuthnChallengeMode, target: target.AuthnChallengeMode},
		{field: "trusted_remote_session_issuer_id", source: nullableUUIDString(source.TrustedRemoteSessionIssuerID), target: nullableUUIDString(target.TrustedRemoteSessionIssuerID)},
		{field: "trusted_remote_session_client_id", source: nullableUUIDString(source.TrustedRemoteSessionClientID), target: nullableUUIDString(target.TrustedRemoteSessionClientID)},
	}
	warnings := make([]*orggen.UserSessionIssuerFieldMismatch, 0, len(settings))
	for _, setting := range settings {
		if setting.source != setting.target {
			warnings = append(warnings, &orggen.UserSessionIssuerFieldMismatch{Field: setting.field, SourceValue: setting.source, TargetValue: setting.target})
		}
	}
	return warnings
}

func userSessionMigrationWarningsFingerprint(sourceID, targetID uuid.UUID, warnings []*orggen.UserSessionIssuerFieldMismatch) string {
	if len(warnings) == 0 {
		return ""
	}

	hash := sha256.New()
	for _, value := range []string{sourceID.String(), targetID.String()} {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	for _, warning := range warnings {
		for _, value := range []string{warning.Field, warning.SourceValue, warning.TargetValue} {
			_, _ = hash.Write([]byte(value))
			_, _ = hash.Write([]byte{0})
		}
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func buildUserSessionMigrationPreflight(ctx context.Context, q *repo.Queries, organizationID string, source, target repo.UserSessionIssuer) (userSessionMigrationPreflight, error) {
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
	warnings := userSessionMigrationWarnings(source, target)
	return userSessionMigrationPreflight{
		clientCount: clients, sessionCount: sessions, consentCount: consents, cimdClientCount: cimdClients, remoteSessionCount: remoteSessions,
		conflictingClientIDs: conflicts, principalBindingConflictCount: principalConflicts, emaBindingConflictCount: emaConflicts,
		platformOwned: sourceOwned || targetOwned, warnings: warnings,
		warningsFingerprint: userSessionMigrationWarningsFingerprint(source.ID, target.ID, warnings),
	}, nil
}

func userSessionMigrationPreflightView(preflight userSessionMigrationPreflight) *orggen.OrganizationUserSessionIssuerMigratePreflight {
	return &orggen.OrganizationUserSessionIssuerMigratePreflight{
		ClientCount: int(preflight.clientCount), SessionCount: int(preflight.sessionCount), ConsentCount: int(preflight.consentCount), CimdClientCount: int(preflight.cimdClientCount), RemoteSessionCount: int(preflight.remoteSessionCount),
		ConflictingClientIds: preflight.conflictingClientIDs, PrincipalBindingConflictCount: int(preflight.principalBindingConflictCount), EmaBindingConflictCount: int(preflight.emaBindingConflictCount), PlatformOwned: preflight.platformOwned,
		Warnings: preflight.warnings, WarningsFingerprint: preflight.warningsFingerprint, CanMigrate: preflight.canMigrate(),
	}
}

func (s *Service) GetIssuerMigratePreflight(ctx context.Context, payload *orggen.GetIssuerMigratePreflightPayload) (*orggen.OrganizationUserSessionIssuerMigratePreflight, error) {
	authCtx, err := s.requireOrganizationIssuerScope(ctx, authz.ScopeOrgRead)
	if err != nil {
		return nil, err
	}
	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
	sourceID, targetID, err := parseUserSessionMigrationIDs(payload.SourceID, payload.TargetID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "%v", err).LogError(ctx, logger)
	}
	source, target, err := loadUserSessionMigrationPair(ctx, repo.New(s.db), authCtx.ActiveOrganizationID, sourceID, targetID, false)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "source or target user session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeBadRequest, err, "%v", err).LogError(ctx, logger)
	}
	preflight, err := buildUserSessionMigrationPreflight(ctx, repo.New(s.db), authCtx.ActiveOrganizationID, source, target)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build user session issuer migrate preflight").LogError(ctx, logger)
	}
	return userSessionMigrationPreflightView(preflight), nil
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
	q := repo.New(dbtx)
	source, target, err := loadUserSessionMigrationPair(ctx, q, authCtx.ActiveOrganizationID, sourceID, targetID, false)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "source or target user session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeBadRequest, err, "%v", err).LogError(ctx, logger)
	}
	if err := lockUserSessionIssuersForMigration(ctx, q, source.ID, target.ID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock issuers for migration").LogError(ctx, logger)
	}
	source, target, err = loadUserSessionMigrationPair(ctx, q, authCtx.ActiveOrganizationID, sourceID, targetID, true)
	if err != nil {
		return nil, oops.E(oops.CodeConflict, err, "issuer scope changed while migration was being locked; retry").LogError(ctx, logger)
	}
	for _, issuer := range []repo.UserSessionIssuer{source, target} {
		if issuer.ProjectID.Valid && (!issuer.OrganizationID.Valid || issuer.OrganizationID.String != authCtx.ActiveOrganizationID) {
			if _, err := q.NormalizeLegacyProjectUserSessionIssuerOrganization(ctx, repo.NormalizeLegacyProjectUserSessionIssuerOrganizationParams{ID: issuer.ID, OrganizationID: authCtx.ActiveOrganizationID}); err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "normalize project user session issuer organization").LogError(ctx, logger)
			}
		}
	}
	source, target, err = loadUserSessionMigrationPair(ctx, q, authCtx.ActiveOrganizationID, sourceID, targetID, true)
	if err != nil {
		return nil, oops.E(oops.CodeConflict, err, "issuer scope changed while migration was normalized; retry").LogError(ctx, logger)
	}
	if err := q.LockUserSessionIssuerClientsForMigration(ctx, source.ID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock source user session clients").LogError(ctx, logger)
	}
	preflight, err := buildUserSessionMigrationPreflight(ctx, q, authCtx.ActiveOrganizationID, source, target)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build user session issuer migrate preflight").LogError(ctx, logger)
	}
	if !preflight.canMigrate() {
		return nil, oops.E(oops.CodeConflict, nil, "user session issuer migration has unresolved blockers; refresh the preflight").LogError(ctx, logger)
	}
	if preflight.warningsFingerprint != "" && (payload.ConfirmedWarningsFingerprint == nil || *payload.ConfirmedWarningsFingerprint != preflight.warningsFingerprint) {
		return nil, oops.E(oops.CodeConflict, nil, "user session issuer migration warnings have not been confirmed or changed; refresh the preflight and confirm its exact warnings fingerprint").LogError(ctx, logger)
	}

	targetProjectID := target.ProjectID
	consentsMigrated, err := q.UpdateUserSessionConsentsToIssuerScope(ctx, repo.UpdateUserSessionConsentsToIssuerScopeParams{TargetProjectID: targetProjectID, TargetIssuerID: target.ID, OrganizationID: authCtx.ActiveOrganizationID, SourceIssuerID: source.ID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "normalize migrated user session consents").LogError(ctx, logger)
	}
	clientsMigrated, err := q.UpdateUserSessionClientsToIssuer(ctx, repo.UpdateUserSessionClientsToIssuerParams{TargetIssuerID: target.ID, TargetProjectID: targetProjectID, OrganizationID: authCtx.ActiveOrganizationID, SourceIssuerID: source.ID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "migrate user session clients").LogError(ctx, logger)
	}
	sessionsMigrated, err := q.UpdateUserSessionsToIssuer(ctx, repo.UpdateUserSessionsToIssuerParams{TargetIssuerID: target.ID, TargetProjectID: targetProjectID, OrganizationID: authCtx.ActiveOrganizationID, SourceIssuerID: source.ID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "migrate user sessions").LogError(ctx, logger)
	}
	if _, err := q.MergeDuplicateUserSessionIssuerCimdClients(ctx, repo.MergeDuplicateUserSessionIssuerCimdClientsParams{SourceIssuerID: source.ID, TargetIssuerID: target.ID, OrganizationID: authCtx.ActiveOrganizationID}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "merge duplicate CIMD clients").LogError(ctx, logger)
	}
	cimdClientsMigrated, err := q.UpdateUserSessionIssuerCimdClientsToIssuer(ctx, repo.UpdateUserSessionIssuerCimdClientsToIssuerParams{TargetIssuerID: target.ID, TargetProjectID: targetProjectID, OrganizationID: authCtx.ActiveOrganizationID, SourceIssuerID: source.ID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "migrate CIMD clients").LogError(ctx, logger)
	}
	if _, err := q.InsertTargetRemoteSessionClientIssuerLinks(ctx, repo.InsertTargetRemoteSessionClientIssuerLinksParams{TargetIssuerID: target.ID, SourceIssuerID: source.ID, OrganizationID: authCtx.ActiveOrganizationID}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create target remote-session client links").LogError(ctx, logger)
	}
	remoteSessionsMigrated, err := q.UpdateRemoteSessionsToUserSessionIssuer(ctx, repo.UpdateRemoteSessionsToUserSessionIssuerParams{TargetIssuerID: target.ID, SourceIssuerID: source.ID, OrganizationID: authCtx.ActiveOrganizationID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "migrate remote-session provenance").LogError(ctx, logger)
	}
	if _, err := q.DeleteSourceRemoteSessionClientIssuerLinks(ctx, repo.DeleteSourceRemoteSessionClientIssuerLinksParams{SourceIssuerID: source.ID, TargetIssuerID: target.ID, OrganizationID: authCtx.ActiveOrganizationID}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "remove source remote-session client links").LogError(ctx, logger)
	}
	if _, err := q.UpdateRemoteSessionEMABindingsToUserSessionIssuer(ctx, repo.UpdateRemoteSessionEMABindingsToUserSessionIssuerParams{TargetIssuerID: target.ID, SourceIssuerID: source.ID, OrganizationID: authCtx.ActiveOrganizationID}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "migrate EMA bindings").LogError(ctx, logger)
	}
	ownerUpdates := []struct {
		name string
		fn   func() (int64, error)
	}{
		{name: "MCP servers", fn: func() (int64, error) {
			return q.UpdateMCPServersToUserSessionIssuer(ctx, repo.UpdateMCPServersToUserSessionIssuerParams{TargetIssuerID: conv.ToNullUUID(target.ID), SourceIssuerID: conv.ToNullUUID(source.ID), OrganizationID: authCtx.ActiveOrganizationID})
		}},
		{name: "toolsets", fn: func() (int64, error) {
			return q.UpdateToolsetsToUserSessionIssuer(ctx, repo.UpdateToolsetsToUserSessionIssuerParams{TargetIssuerID: conv.ToNullUUID(target.ID), SourceIssuerID: conv.ToNullUUID(source.ID), OrganizationID: authCtx.ActiveOrganizationID})
		}},
		{name: "meta MCP servers", fn: func() (int64, error) {
			return q.UpdateMetaMCPServersToUserSessionIssuer(ctx, repo.UpdateMetaMCPServersToUserSessionIssuerParams{TargetIssuerID: conv.ToNullUUID(target.ID), SourceIssuerID: conv.ToNullUUID(source.ID), OrganizationID: authCtx.ActiveOrganizationID})
		}},
		{name: "Platform MCP registrations", fn: func() (int64, error) {
			return q.UpdatePlatformMCPRegistrationsToUserSessionIssuer(ctx, repo.UpdatePlatformMCPRegistrationsToUserSessionIssuerParams{TargetIssuerID: conv.ToNullUUID(target.ID), SourceIssuerID: conv.ToNullUUID(source.ID), OrganizationID: authCtx.ActiveOrganizationID})
		}},
	}
	for _, operation := range ownerUpdates {
		if _, err := operation.fn(); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "migrate user session issuer %s", operation.name).LogError(ctx, logger)
		}
	}
	deleted, err := q.SoftDeleteMigratedUserSessionIssuer(ctx, repo.SoftDeleteMigratedUserSessionIssuerParams{SourceIssuerID: source.ID, TargetIssuerID: target.ID, OrganizationID: authCtx.ActiveOrganizationID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "soft-delete migrated user session issuer").LogError(ctx, logger)
	}
	targetView := UserSessionIssuerView(target)
	if err := s.audit.LogUserSessionIssuerMigrate(ctx, dbtx, audit.LogUserSessionIssuerMigrateEvent{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: projectIDValue(deleted.ProjectID),
		Actor: urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), ActorDisplayName: authCtx.Email, ActorSlug: nil,
		SourceUserSessionIssuerURN: urn.NewUserSessionIssuer(source.ID), SourceSlug: source.Slug,
		TargetUserSessionIssuerURN: urn.NewUserSessionIssuer(target.ID), TargetSlug: target.Slug,
		ClientsMigrated: clientsMigrated, SessionsMigrated: sessionsMigrated, ConsentsMigrated: consentsMigrated, CimdClientsMigrated: cimdClientsMigrated, RemoteSessionsMigrated: remoteSessionsMigrated,
		UserSessionIssuerSnapshotBefore: UserSessionIssuerView(source), UserSessionIssuerSnapshotAfter: targetView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log user session issuer migration").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}
	return &orggen.MigrateOrganizationUserSessionIssuerResult{
		Issuer: targetView, ClientsMigrated: int(clientsMigrated), SessionsMigrated: int(sessionsMigrated), ConsentsMigrated: int(consentsMigrated), CimdClientsMigrated: int(cimdClientsMigrated), RemoteSessionsMigrated: int(remoteSessionsMigrated), SourceDeleted: true,
	}, nil
}
