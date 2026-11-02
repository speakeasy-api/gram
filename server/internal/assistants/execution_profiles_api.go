package assistants

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/assistants"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/executionprofiles"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func (s *Service) ListExecutionProfiles(ctx context.Context, payload *gen.ListExecutionProfilesPayload) (*gen.ListExecutionProfilesResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	assistantID, err := uuid.Parse(payload.AssistantID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid assistant id").LogWarn(ctx, s.logger)
	}
	if err := s.authz.Require(ctx, authz.AssistantCheck(authz.ScopeAssistantRead, assistantID.String(), authCtx.ProjectID.String())); err != nil {
		return nil, err
	}
	record, err := s.core.GetAssistant(ctx, *authCtx.ProjectID, assistantID)
	if err != nil {
		return nil, mapAssistantStoreError(ctx, s.logger, err, "get assistant")
	}
	triggers, err := executionprofiles.New(s.core.db).ListAssistantProfileTargets(ctx, executionprofiles.ListAssistantProfileTargetsParams{
		OrganizationID: record.OrganizationID, ProjectID: record.ProjectID, AssistantID: record.ID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list assistant triggers").LogError(ctx, s.logger)
	}
	profiles := make([]*gen.AssistantExecutionProfile, 0, len(triggers)*len(executionprofiles.Contexts))
	for _, trigger := range triggers {
		for _, execCtx := range executionprofiles.Contexts {
			view, err := executionprofiles.Describe(ctx, s.core.db, executionprofiles.Target{
				OrganizationID: record.OrganizationID, ProjectID: record.ProjectID, AssistantID: record.ID,
				TriggerID: trigger.TriggerID, Context: execCtx,
			})
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "describe execution profile").LogError(ctx, s.logger)
			}
			profiles = append(profiles, toHTTPExecutionProfile(trigger.TriggerName, view))
		}
	}
	return &gen.ListExecutionProfilesResult{Profiles: profiles}, nil
}

func (s *Service) SetExecutionProfileChoice(ctx context.Context, payload *gen.SetExecutionProfileChoicePayload) (*gen.AssistantExecutionProfile, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if authCtx.UserID == "" {
		return nil, oops.E(oops.CodeUnauthorized, nil, "changing an assistant's credentials requires a user identity")
	}
	assistantID, err := uuid.Parse(payload.AssistantID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid assistant id").LogWarn(ctx, s.logger)
	}
	triggerID, err := uuid.Parse(payload.TriggerInstanceID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid trigger instance id").LogWarn(ctx, s.logger)
	}
	serverID, err := uuid.Parse(payload.McpServerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp server id").LogWarn(ctx, s.logger)
	}
	input := executionprofiles.ChoiceInput{AuthClientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, NoAuth: payload.NoAuth != nil && *payload.NoAuth, Clear: payload.Automatic != nil && *payload.Automatic, ExpectedVersion: payload.ExpectedVersion}
	if payload.AuthClientID != nil {
		clientID, err := uuid.Parse(*payload.AuthClientID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid auth client id").LogWarn(ctx, s.logger)
		}
		input.AuthClientID = uuid.NullUUID{UUID: clientID, Valid: true}
	}
	modes := 0
	for _, set := range []bool{input.AuthClientID.Valid, input.NoAuth, input.Clear} {
		if set {
			modes++
		}
	}
	if modes != 1 {
		return nil, oops.E(oops.CodeBadRequest, nil, "name exactly one of auth_client_id, no_auth or automatic").LogWarn(ctx, s.logger)
	}
	if err := s.authz.Require(ctx, authz.AssistantCheck(authz.ScopeAssistantWrite, assistantID.String(), authCtx.ProjectID.String())); err != nil {
		return nil, err
	}
	record, err := s.core.GetAssistant(ctx, *authCtx.ProjectID, assistantID)
	if err != nil {
		return nil, mapAssistantStoreError(ctx, s.logger, err, "get assistant")
	}
	triggers, err := executionprofiles.New(s.core.db).ListAssistantProfileTargets(ctx, executionprofiles.ListAssistantProfileTargetsParams{
		OrganizationID: record.OrganizationID, ProjectID: record.ProjectID, AssistantID: record.ID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list assistant triggers").LogError(ctx, s.logger)
	}
	triggerName := ""
	for _, trigger := range triggers {
		if trigger.TriggerID == triggerID {
			triggerName = trigger.TriggerName
		}
	}
	if triggerName == "" {
		return nil, oops.E(oops.CodeNotFound, nil, "the assistant's turns do not run under this trigger").LogWarn(ctx, s.logger)
	}
	target := executionprofiles.Target{
		OrganizationID: record.OrganizationID, ProjectID: record.ProjectID, AssistantID: record.ID,
		TriggerID: triggerID, Context: executionprofiles.Context(payload.ExecutionContext),
	}

	var after *audit.AssistantExecutionProfileChoice
	if !input.Clear {
		after = &audit.AssistantExecutionProfileChoice{AuthClientID: "", Source: upstreamclients.SelectionSourceAdmin}
		if input.AuthClientID.Valid && !input.NoAuth {
			after.AuthClientID = input.AuthClientID.UUID.String()
		}
	}
	err = executionprofiles.SetChoice(ctx, s.core.db, target, serverID, input, func(ctx context.Context, tx pgx.Tx, previous *executionprofiles.Choice) error {
		if err := s.core.audit.LogAssistantExecutionProfileUpdate(ctx, tx, audit.LogAssistantExecutionProfileUpdateEvent{
			OrganizationID: record.OrganizationID, ProjectID: record.ProjectID,
			Actor: urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), ActorDisplayName: authCtx.Email, ActorSlug: nil,
			AssistantURN: urn.NewAssistant(record.ID), AssistantName: record.Name,
			TriggerURN: urn.NewTriggerInstance(triggerID), ExecutionContext: payload.ExecutionContext, McpServerURN: urn.NewMcpServer(serverID),
			ChoiceBefore: auditChoice(previous), ChoiceAfter: after,
		}); err != nil {
			return fmt.Errorf("log execution profile change: %w", err)
		}
		return nil
	})
	switch {
	case errors.Is(err, executionprofiles.ErrServerNotReached):
		return nil, oops.E(oops.CodeNotFound, err, "%s", executionprofiles.ErrServerNotReached.Error()).LogWarn(ctx, s.logger)
	case errors.Is(err, executionprofiles.ErrChoiceChanged):
		return nil, oops.E(oops.CodeConflict, err, "%s", executionprofiles.ErrChoiceChanged.Error()).LogWarn(ctx, s.logger)
	case errors.Is(err, executionprofiles.ErrVersionChanged):
		return nil, oops.E(oops.CodeConflict, err, "%s", executionprofiles.ErrVersionChanged.Error()).LogWarn(ctx, s.logger)
	case errors.Is(err, upstreamclients.ErrChoicePersonal):
		return nil, oops.E(oops.CodeBadRequest, err, "turns that act for nobody can use shared credentials only").LogWarn(ctx, s.logger)
	case errors.Is(err, upstreamclients.ErrChoiceNotOffered):
		return nil, oops.E(oops.CodeBadRequest, err, "%s", upstreamclients.ErrChoiceNotOffered.Error()).LogWarn(ctx, s.logger)
	case errors.Is(err, upstreamclients.ErrChoiceNeedsCredential):
		return nil, oops.E(oops.CodeBadRequest, err, "%s", upstreamclients.ErrChoiceNeedsCredential.Error()).LogWarn(ctx, s.logger)
	case errors.Is(err, upstreamclients.ErrChoiceNeedsNoCredential):
		return nil, oops.E(oops.CodeBadRequest, err, "%s", upstreamclients.ErrChoiceNeedsNoCredential.Error()).LogWarn(ctx, s.logger)
	case errors.Is(err, upstreamclients.ErrChoiceUpstreamAuthPending):
		return nil, oops.E(oops.CodeBadRequest, err, "%s", upstreamclients.ErrChoiceUpstreamAuthPending.Error()).LogWarn(ctx, s.logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "change execution profile").LogError(ctx, s.logger)
	}

	view, err := executionprofiles.Describe(ctx, s.core.db, target)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "describe execution profile").LogError(ctx, s.logger)
	}
	return toHTTPExecutionProfile(triggerName, view), nil
}

func auditChoice(choice *executionprofiles.Choice) *audit.AssistantExecutionProfileChoice {
	if choice == nil {
		return nil
	}
	out := &audit.AssistantExecutionProfileChoice{AuthClientID: "", Source: choice.Source}
	if choice.AuthClientID.Valid {
		out.AuthClientID = choice.AuthClientID.UUID.String()
	}
	return out
}

func toHTTPExecutionProfile(triggerName string, view executionprofiles.View) *gen.AssistantExecutionProfile {
	servers := make([]*gen.ExecutionProfileServer, 0, len(view.Servers))
	for _, server := range view.Servers {
		out := &gen.ExecutionProfileServer{
			McpServerID:   server.MCPServerID.String(),
			Chosen:        server.Choice != nil,
			AuthClientID:  nil,
			Source:        nil,
			GapReason:     nil,
			Options:       make([]*gen.ExecutionProfileOption, 0, len(server.Options)),
			NoAuthAllowed: server.NoAuthAllowed,
		}
		if server.Choice != nil {
			out.Source = new(gen.ExecutionProfileChoiceSource(server.Choice.Source))
			if server.Choice.AuthClientID.Valid {
				out.AuthClientID = new(server.Choice.AuthClientID.UUID.String())
			}
		}
		if server.Gap != nil {
			out.GapReason = new(string(*server.Gap))
		}
		for _, option := range server.Options {
			out.Options = append(out.Options, &gen.ExecutionProfileOption{AuthClientID: option.AuthClientID.String(), Name: option.Name, ClientType: option.ClientType})
		}
		servers = append(servers, out)
	}
	return &gen.AssistantExecutionProfile{
		TriggerInstanceID: view.Target.TriggerID.String(),
		TriggerName:       triggerName,
		ExecutionContext:  string(view.Target.Context),
		Version:           view.Version,
		Servers:           servers,
	}
}
