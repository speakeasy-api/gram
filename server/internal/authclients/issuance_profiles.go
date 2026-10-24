package authclients

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/auth_clients"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func (s *Service) ListIssuanceProfiles(ctx context.Context, payload *gen.ListIssuanceProfilesPayload) (*gen.ListIssuanceProfilesResult, error) {
	authCtx, logger, err := s.require(ctx, authz.ScopeOrgRead)
	if err != nil {
		return nil, err
	}
	issuerID, err := uuid.Parse(payload.UserSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid user session issuer id").LogWarn(ctx, logger)
	}
	queries := repo.New(s.db)
	if _, err := queries.GetOrganizationUserSessionIssuer(ctx, repo.GetOrganizationUserSessionIssuerParams{ID: issuerID, OrganizationID: authCtx.ActiveOrganizationID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "sign-in not found").LogWarn(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get sign-in").LogError(ctx, logger)
	}
	profiles, err := queries.ListIssuanceProfiles(ctx, repo.ListIssuanceProfilesParams{UserSessionIssuerID: issuerID, OrganizationID: authCtx.ActiveOrganizationID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list issuance profiles").LogError(ctx, logger)
	}
	views, err := issuanceProfileViews(ctx, queries, authCtx.ActiveOrganizationID, profiles)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build issuance profile views").LogError(ctx, logger)
	}
	return &gen.ListIssuanceProfilesResult{Items: views}, nil
}

func (s *Service) SetIssuanceProfile(ctx context.Context, payload *gen.SetIssuanceProfilePayload) (*types.IssuanceProfile, error) {
	authCtx, logger, err := s.require(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}
	organizationID := authCtx.ActiveOrganizationID

	issuerID, err := uuid.Parse(payload.UserSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid user session issuer id").LogWarn(ctx, logger)
	}
	clientID, err := parseOptionalUUID(payload.UserSessionClientID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid user session client id").LogWarn(ctx, logger)
	}
	workloadIssuerID, err := parseOptionalUUID(payload.WorkloadIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid workload issuer id").LogWarn(ctx, logger)
	}
	workloadSubject := strings.TrimSpace(conv.PtrValOr(payload.WorkloadSubject, ""))
	workload := workloadIssuerID.Valid || workloadSubject != ""
	switch {
	case clientID.Valid == workload:
		return nil, oops.E(oops.CodeBadRequest, nil, "a profile applies to exactly one caller: an OAuth client, or a workload issuer and subject").LogWarn(ctx, logger)
	case workload && (!workloadIssuerID.Valid || workloadSubject == ""):
		return nil, oops.E(oops.CodeBadRequest, nil, "a workload's profile needs both its issuer and its subject").LogWarn(ctx, logger)
	}
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "a profile needs a name").LogWarn(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)

	issuer, err := queries.GetOrganizationUserSessionIssuer(ctx, repo.GetOrganizationUserSessionIssuerParams{ID: issuerID, OrganizationID: organizationID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "sign-in not found").LogWarn(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get sign-in").LogError(ctx, logger)
	}
	if clientID.Valid {
		ok, err := queries.IsUserSessionClientOfIssuer(ctx, repo.IsUserSessionClientOfIssuerParams{ID: clientID.UUID, UserSessionIssuerID: issuerID})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "check client").LogError(ctx, logger)
		}
		if !ok {
			return nil, oops.E(oops.CodeNotFound, nil, "the client does not sign in through this sign-in").LogWarn(ctx, logger)
		}
	} else {
		ok, err := queries.IsOrganizationWorkloadIssuer(ctx, repo.IsOrganizationWorkloadIssuerParams{ID: workloadIssuerID.UUID, OrganizationID: organizationID})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "check workload issuer").LogError(ctx, logger)
		}
		if !ok {
			return nil, oops.E(oops.CodeNotFound, nil, "workload issuer not found").LogWarn(ctx, logger)
		}
	}

	existing, err := queries.LockIssuanceProfileForCaller(ctx, repo.LockIssuanceProfileForCallerParams{
		UserSessionIssuerID: issuerID, OrganizationID: organizationID,
		UserSessionClientID: clientID, WorkloadIssuerID: workloadIssuerID, WorkloadSubject: conv.ToPGTextEmpty(workloadSubject),
	})
	creating := errors.Is(err, pgx.ErrNoRows)
	if err != nil && !creating {
		return nil, oops.E(oops.CodeUnexpected, err, "lock issuance profile").LogError(ctx, logger)
	}

	var before *types.IssuanceProfile
	var profile repo.IssuanceSelectionProfile
	if creating {
		profile, err = queries.CreateIssuanceProfile(ctx, repo.CreateIssuanceProfileParams{
			OrganizationID: organizationID, ProjectID: issuer.ProjectID, UserSessionIssuerID: issuerID,
			UserSessionClientID: clientID, WorkloadIssuerID: workloadIssuerID, WorkloadSubject: conv.ToPGTextEmpty(workloadSubject),
			Name: name, CreatedByUserID: conv.ToPGTextEmpty(authCtx.UserID),
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "create issuance profile").LogError(ctx, logger)
		}
	} else {
		views, err := issuanceProfileViews(ctx, queries, organizationID, []repo.IssuanceSelectionProfile{existing})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "build issuance profile view").LogError(ctx, logger)
		}
		before = views[0]
		profile, err = queries.RenameIssuanceProfile(ctx, repo.RenameIssuanceProfileParams{Name: name, ID: existing.ID, OrganizationID: organizationID})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "update issuance profile").LogError(ctx, logger)
		}
		if err := queries.ClearIssuanceProfileChoices(ctx, repo.ClearIssuanceProfileChoicesParams{ProfileID: profile.ID, OrganizationID: organizationID}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "clear issuance profile choices").LogError(ctx, logger)
		}
	}

	if err := recordProfileChoices(ctx, dbtx, organizationID, profile.ID, workload, payload.Choices); err != nil {
		return nil, err
	}

	views, err := issuanceProfileViews(ctx, queries, organizationID, []repo.IssuanceSelectionProfile{profile})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build issuance profile view").LogError(ctx, logger)
	}
	after := views[0]

	event := audit.LogIssuanceSelectionProfileEvent{
		OrganizationID: organizationID, ProjectID: profile.ProjectID.UUID,
		Actor: urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), ActorDisplayName: authCtx.Email, ActorSlug: nil,
		IssuanceSelectionProfileURN: urn.NewIssuanceSelectionProfile(profile.ID), IssuanceSelectionProfileName: profile.Name,
		IssuanceSelectionProfileSnapshotBefore: before, IssuanceSelectionProfileSnapshotAfter: after,
	}
	logEvent := s.audit.LogIssuanceSelectionProfileUpdate
	if creating {
		logEvent = s.audit.LogIssuanceSelectionProfileCreate
	}
	if err := logEvent(ctx, dbtx, event); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log issuance profile change").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}
	return after, nil
}

func (s *Service) DeleteIssuanceProfile(ctx context.Context, payload *gen.DeleteIssuanceProfilePayload) error {
	authCtx, logger, err := s.require(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return err
	}
	profileID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid issuance profile id").LogWarn(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)

	profile, err := queries.LockIssuanceProfile(ctx, repo.LockIssuanceProfileParams{ID: profileID, OrganizationID: authCtx.ActiveOrganizationID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "issuance profile not found").LogWarn(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "lock issuance profile").LogError(ctx, logger)
	}
	views, err := issuanceProfileViews(ctx, queries, authCtx.ActiveOrganizationID, []repo.IssuanceSelectionProfile{profile})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "build issuance profile view").LogError(ctx, logger)
	}
	if err := queries.ClearIssuanceProfileChoices(ctx, repo.ClearIssuanceProfileChoicesParams{ProfileID: profile.ID, OrganizationID: authCtx.ActiveOrganizationID}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "clear issuance profile choices").LogError(ctx, logger)
	}
	if err := queries.DeleteIssuanceProfile(ctx, repo.DeleteIssuanceProfileParams{ID: profile.ID, OrganizationID: authCtx.ActiveOrganizationID}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete issuance profile").LogError(ctx, logger)
	}
	if err := s.audit.LogIssuanceSelectionProfileDelete(ctx, dbtx, audit.LogIssuanceSelectionProfileEvent{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: profile.ProjectID.UUID,
		Actor: urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), ActorDisplayName: authCtx.Email, ActorSlug: nil,
		IssuanceSelectionProfileURN: urn.NewIssuanceSelectionProfile(profile.ID), IssuanceSelectionProfileName: profile.Name,
		IssuanceSelectionProfileSnapshotBefore: views[0], IssuanceSelectionProfileSnapshotAfter: nil,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log issuance profile delete").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}
	return nil
}

// recordProfileChoices validates and stores a profile's choices. A workload
// profile represents nobody, so it may name shared credentials only.
func recordProfileChoices(ctx context.Context, dbtx pgx.Tx, organizationID string, profileID uuid.UUID, sharedOnly bool, choices []*types.IssuanceProfileChoice) error {
	queries := upstreamclientsrepo.New(dbtx)
	seen := make(map[uuid.UUID]bool, len(choices))
	for _, choice := range choices {
		serverID, err := uuid.Parse(choice.McpServerID)
		if err != nil {
			return oops.E(oops.CodeBadRequest, err, "invalid mcp server id")
		}
		if seen[serverID] {
			return oops.E(oops.CodeBadRequest, nil, "a server can have only one choice")
		}
		seen[serverID] = true
		clientID, err := parseOptionalUUID(choice.AuthClientID)
		if err != nil {
			return oops.E(oops.CodeBadRequest, err, "invalid auth client id")
		}
		offered, err := queries.ListServerConsentAuthClients(ctx, upstreamclientsrepo.ListServerConsentAuthClientsParams{OrganizationID: organizationID, McpServerIds: []uuid.UUID{serverID}})
		if err != nil {
			return fmt.Errorf("list server auth clients: %w", err)
		}
		if len(offered) == 0 {
			return oops.E(oops.CodeNotFound, nil, "mcp server not found")
		}
		if err := upstreamclients.CheckConfiguredChoice(offered, clientID, sharedOnly); err != nil {
			return oops.E(oops.CodeBadRequest, err, "%s", err.Error())
		}
		recorded, err := queries.RecordAuthClientChoice(ctx, upstreamclientsrepo.RecordAuthClientChoiceParams{
			CredentialKind: upstreamclients.CredentialKindIssuanceProfile, CredentialID: profileID,
			Source: upstreamclients.SelectionSourceIssuance, OrganizationID: organizationID,
			AuthClientID: clientID, McpServerID: serverID,
		})
		if err != nil {
			return fmt.Errorf("record issuance profile choice: %w", err)
		}
		if recorded != 1 {
			return oops.E(oops.CodeConflict, nil, "the server's credentials changed; review the choice and try again")
		}
	}
	return nil
}

// issuanceProfileViews renders profiles with their choices.
func issuanceProfileViews(ctx context.Context, queries *repo.Queries, organizationID string, profiles []repo.IssuanceSelectionProfile) ([]*types.IssuanceProfile, error) {
	ids := make([]uuid.UUID, 0, len(profiles))
	for _, p := range profiles {
		ids = append(ids, p.ID)
	}
	rows, err := queries.ListIssuanceProfileChoices(ctx, repo.ListIssuanceProfileChoicesParams{ProfileIds: ids, OrganizationID: organizationID})
	if err != nil {
		return nil, fmt.Errorf("list issuance profile choices: %w", err)
	}
	byProfile := map[uuid.UUID][]*types.IssuanceProfileChoice{}
	for _, row := range rows {
		choice := &types.IssuanceProfileChoice{McpServerID: row.McpServerID.String(), AuthClientID: nil}
		if row.AuthClientID.Valid {
			choice.AuthClientID = new(row.AuthClientID.UUID.String())
		}
		byProfile[row.ProfileID] = append(byProfile[row.ProfileID], choice)
	}
	views := make([]*types.IssuanceProfile, 0, len(profiles))
	for _, p := range profiles {
		choices := byProfile[p.ID]
		if choices == nil {
			choices = []*types.IssuanceProfileChoice{}
		}
		slices.SortFunc(choices, func(a, b *types.IssuanceProfileChoice) int { return strings.Compare(a.McpServerID, b.McpServerID) })
		views = append(views, &types.IssuanceProfile{
			ID:                  p.ID.String(),
			Name:                p.Name,
			UserSessionIssuerID: p.UserSessionIssuerID.String(),
			UserSessionClientID: conv.FromNullableUUID(p.UserSessionClientID),
			WorkloadIssuerID:    conv.FromNullableUUID(p.WorkloadIssuerID),
			WorkloadSubject:     conv.FromPGText[string](p.WorkloadSubject),
			Choices:             choices,
			UpdatedAt:           p.UpdatedAt.Time.Format(time.RFC3339),
		})
	}
	return views, nil
}
