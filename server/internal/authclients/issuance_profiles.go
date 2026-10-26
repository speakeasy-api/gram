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
	"github.com/jackc/pgx/v5/pgxpool"

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
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "a profile needs a name").LogWarn(ctx, logger)
	}
	choices := func([]*types.IssuanceProfileChoice) []*types.IssuanceProfileChoice { return payload.Choices }
	if payload.McpServerID != nil {
		serverID := *payload.McpServerID
		if len(payload.Choices) > 1 || (len(payload.Choices) == 1 && (payload.Choices[0] == nil || payload.Choices[0].McpServerID != serverID)) {
			return nil, oops.E(oops.CodeBadRequest, nil, "a write limited to one MCP server holds at most that server's choice").LogWarn(ctx, logger)
		}
		choices = func(current []*types.IssuanceProfileChoice) []*types.IssuanceProfileChoice {
			next := make([]*types.IssuanceProfileChoice, 0, len(current)+1)
			for _, choice := range current {
				if choice.McpServerID != serverID {
					next = append(next, choice)
				}
			}
			return append(next, payload.Choices...)
		}
	}
	profile, err := WriteIssuanceProfile(ctx, s.db, s.audit, IssuanceProfileWrite{
		OrganizationID: authCtx.ActiveOrganizationID, ActorUserID: authCtx.UserID, ActorDisplayName: authCtx.Email,
		UserSessionIssuerID: issuerID, ClientID: clientID, WorkloadIssuerID: workloadIssuerID,
		WorkloadSubject: conv.PtrValOr(payload.WorkloadSubject, ""), Name: name,
		Choices: choices,
	})
	if err != nil {
		if _, ok := errors.AsType[*oops.ShareableError](err); ok {
			return nil, err
		}
		return nil, oops.E(oops.CodeUnexpected, err, "write issuance profile").LogError(ctx, logger)
	}
	return profile, nil
}

// IssuanceProfileWrite is one change to a headless caller's issuance profile:
// an OAuth client, or a workload issuer and subject, on one sign-in.
type IssuanceProfileWrite struct {
	OrganizationID   string
	ActorUserID      string
	ActorDisplayName *string

	UserSessionIssuerID uuid.UUID
	ClientID            uuid.NullUUID
	WorkloadIssuerID    uuid.NullUUID
	// WorkloadSubject is an opaque identity key, compared exactly.
	WorkloadSubject string

	// Name names a new profile and renames an existing one; empty keeps an
	// existing profile's name.
	Name string

	// Choices returns every choice the profile should hold, given the ones it
	// holds now (none for a new profile).
	Choices func(current []*types.IssuanceProfileChoice) []*types.IssuanceProfileChoice
}

// WriteIssuanceProfile creates or updates a caller's profile and records the
// audit event in the same transaction. Writes for one caller are serialized,
// so a change computed from the current choices never loses a concurrent one.
// It performs no authorization: callers check that the actor administers the
// organization. Refusals are *oops.ShareableError.
func WriteIssuanceProfile(ctx context.Context, db *pgxpool.Pool, auditLogger *audit.Logger, w IssuanceProfileWrite) (*types.IssuanceProfile, error) {
	organizationID := w.OrganizationID
	workload := w.WorkloadIssuerID.Valid || w.WorkloadSubject != ""
	switch {
	case w.ClientID.Valid == workload:
		return nil, oops.E(oops.CodeBadRequest, nil, "a profile applies to exactly one caller: an OAuth client, or a workload issuer and subject")
	case workload && (!w.WorkloadIssuerID.Valid || w.WorkloadSubject == ""):
		return nil, oops.E(oops.CodeBadRequest, nil, "a workload's profile needs both its issuer and its subject")
	}

	dbtx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)

	issuer, err := queries.GetOrganizationUserSessionIssuer(ctx, repo.GetOrganizationUserSessionIssuerParams{ID: w.UserSessionIssuerID, OrganizationID: organizationID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeNotFound, err, "sign-in not found")
	}
	if err != nil {
		return nil, fmt.Errorf("get sign-in: %w", err)
	}
	if w.ClientID.Valid {
		ok, err := queries.IsUserSessionClientOfIssuer(ctx, repo.IsUserSessionClientOfIssuerParams{ID: w.ClientID.UUID, UserSessionIssuerID: w.UserSessionIssuerID, OrganizationID: organizationID})
		if err != nil {
			return nil, fmt.Errorf("check client: %w", err)
		}
		if !ok {
			return nil, oops.E(oops.CodeNotFound, nil, "the client does not sign in through this sign-in")
		}
	} else {
		ok, err := queries.IsOrganizationWorkloadIssuer(ctx, repo.IsOrganizationWorkloadIssuerParams{ID: w.WorkloadIssuerID.UUID, OrganizationID: organizationID})
		if err != nil {
			return nil, fmt.Errorf("check workload issuer: %w", err)
		}
		if !ok {
			return nil, oops.E(oops.CodeNotFound, nil, "workload issuer not found")
		}
	}

	callerKey := "client:" + w.ClientID.UUID.String()
	if workload {
		callerKey = "workload:" + w.WorkloadIssuerID.UUID.String() + ":" + w.WorkloadSubject
	}
	if err := queries.LockIssuanceProfileCaller(ctx, repo.LockIssuanceProfileCallerParams{UserSessionIssuerID: w.UserSessionIssuerID.String(), CallerKey: callerKey}); err != nil {
		return nil, fmt.Errorf("lock issuance profile caller: %w", err)
	}
	existing, err := queries.LockIssuanceProfileForCaller(ctx, repo.LockIssuanceProfileForCallerParams{
		UserSessionIssuerID: w.UserSessionIssuerID, OrganizationID: organizationID,
		UserSessionClientID: w.ClientID, WorkloadIssuerID: w.WorkloadIssuerID, WorkloadSubject: conv.ToPGTextEmpty(w.WorkloadSubject),
	})
	creating := errors.Is(err, pgx.ErrNoRows)
	if err != nil && !creating {
		return nil, fmt.Errorf("lock issuance profile: %w", err)
	}

	var before *types.IssuanceProfile
	current := []*types.IssuanceProfileChoice{}
	var profile repo.IssuanceSelectionProfile
	if creating {
		if w.Name == "" {
			return nil, oops.E(oops.CodeBadRequest, nil, "a new profile needs a name")
		}
		profile, err = queries.CreateIssuanceProfile(ctx, repo.CreateIssuanceProfileParams{
			OrganizationID: organizationID, ProjectID: issuer.ProjectID, UserSessionIssuerID: w.UserSessionIssuerID,
			UserSessionClientID: w.ClientID, WorkloadIssuerID: w.WorkloadIssuerID, WorkloadSubject: conv.ToPGTextEmpty(w.WorkloadSubject),
			Name: w.Name, CreatedByUserID: conv.ToPGTextEmpty(w.ActorUserID),
		})
		if err != nil {
			return nil, fmt.Errorf("create issuance profile: %w", err)
		}
	} else {
		views, err := issuanceProfileViews(ctx, queries, organizationID, []repo.IssuanceSelectionProfile{existing})
		if err != nil {
			return nil, fmt.Errorf("build issuance profile view: %w", err)
		}
		before = views[0]
		current = before.Choices
		name := w.Name
		if name == "" {
			name = existing.Name
		}
		profile, err = queries.RenameIssuanceProfile(ctx, repo.RenameIssuanceProfileParams{Name: name, ID: existing.ID, OrganizationID: organizationID})
		if err != nil {
			return nil, fmt.Errorf("update issuance profile: %w", err)
		}
	}

	if err := recordProfileChoices(ctx, dbtx, organizationID, profile.ID, workload, current, w.Choices(current)); err != nil {
		return nil, err
	}

	views, err := issuanceProfileViews(ctx, queries, organizationID, []repo.IssuanceSelectionProfile{profile})
	if err != nil {
		return nil, fmt.Errorf("build issuance profile view: %w", err)
	}
	after := views[0]

	event := audit.LogIssuanceSelectionProfileEvent{
		OrganizationID: organizationID, ProjectID: profile.ProjectID.UUID,
		Actor: urn.NewPrincipal(urn.PrincipalTypeUser, w.ActorUserID), ActorDisplayName: w.ActorDisplayName, ActorSlug: nil,
		IssuanceSelectionProfileURN: urn.NewIssuanceSelectionProfile(profile.ID), IssuanceSelectionProfileName: profile.Name,
		IssuanceSelectionProfileSnapshotBefore: before, IssuanceSelectionProfileSnapshotAfter: after,
	}
	logEvent := auditLogger.LogIssuanceSelectionProfileUpdate
	if creating {
		logEvent = auditLogger.LogIssuanceSelectionProfileCreate
	}
	if err := logEvent(ctx, dbtx, event); err != nil {
		return nil, fmt.Errorf("log issuance profile change: %w", err)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
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

// recordProfileChoices moves a profile from its current choices to the given
// ones. A choice it already holds unchanged is kept without being checked
// again, so a stale choice on one server never blocks an edit to another;
// every new or changed choice is validated. A workload profile represents
// nobody, so it may name shared credentials only.
func recordProfileChoices(ctx context.Context, dbtx pgx.Tx, organizationID string, profileID uuid.UUID, sharedOnly bool, current, choices []*types.IssuanceProfileChoice) error {
	queries := upstreamclientsrepo.New(dbtx)
	profileQueries := repo.New(dbtx)
	held := make(map[string]string, len(current))
	for _, choice := range current {
		held[choice.McpServerID] = conv.PtrValOr(choice.AuthClientID, "")
	}
	clearServer := func(serverID uuid.UUID) error {
		if err := profileQueries.ClearIssuanceProfileServerChoice(ctx, repo.ClearIssuanceProfileServerChoiceParams{ProfileID: profileID, McpServerID: serverID, OrganizationID: organizationID}); err != nil {
			return fmt.Errorf("clear issuance profile choice: %w", err)
		}
		return nil
	}
	seen := make(map[uuid.UUID]bool, len(choices))
	for _, choice := range choices {
		if choice == nil {
			return oops.E(oops.CodeBadRequest, nil, "a choice entry is empty: name the MCP server for each choice")
		}
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
		if heldClient, ok := held[serverID.String()]; ok {
			if clientID.Valid && heldClient == clientID.UUID.String() || !clientID.Valid && heldClient == "" {
				continue
			}
			if err := clearServer(serverID); err != nil {
				return err
			}
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
	for server := range held {
		serverID, err := uuid.Parse(server)
		if err != nil {
			return fmt.Errorf("parse held choice server: %w", err)
		}
		if !seen[serverID] {
			if err := clearServer(serverID); err != nil {
				return err
			}
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

// ListIssuanceProfilesForIssuer lists the profiles of callers signing in
// through one of the organization's sign-ins, with their choices. It performs
// no authorization.
func ListIssuanceProfilesForIssuer(ctx context.Context, db repo.DBTX, organizationID string, issuerID uuid.UUID) ([]*types.IssuanceProfile, error) {
	queries := repo.New(db)
	profiles, err := queries.ListIssuanceProfiles(ctx, repo.ListIssuanceProfilesParams{UserSessionIssuerID: issuerID, OrganizationID: organizationID})
	if err != nil {
		return nil, fmt.Errorf("list issuance profiles: %w", err)
	}
	return issuanceProfileViews(ctx, queries, organizationID, profiles)
}
