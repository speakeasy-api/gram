package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// AssistantRuntimeWorkloadClientConfig specifies credential configuration, not
// authority. TriggerID selects an existing root when there is more than one;
// zero is accepted only when exactly one active root exists. IssuerID identifies
// the MCP authorization server the bootstrap is configuring, never the workload
// trust issuer. Authentication method has no implicit default.
type AssistantRuntimeWorkloadClientConfig struct {
	UserSessionIssuerID uuid.UUID
	TriggerID           uuid.UUID
	AuthMethod          string
	Secret              string
	JWKS                json.RawMessage
	JWKSURI             string
}

// ProvisionAssistantRuntimeWorkloadClient is a trusted worker composition API,
// NOT an HTTP authorization boundary. It resolves the persisted runtime and
// assistant/root identity instead of impersonating an initiating user. Do not
// expose this method to request payloads; use the authorized wrapper for those.
func (s *Service) ProvisionAssistantRuntimeWorkloadClient(ctx context.Context, runtimeID uuid.UUID, c AssistantRuntimeWorkloadClientConfig) (ProvisionedAssistantWorkloadClient, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return ProvisionedAssistantWorkloadClient{}, fmt.Errorf("begin assistant workload client transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	runtime, err := usersessions_repo.New(tx).GetAssistantWorkloadRuntime(ctx, runtimeID)
	if err != nil {
		return ProvisionedAssistantWorkloadClient{}, fmt.Errorf("resolve runtime for lifecycle fence: %w", err)
	}
	if err := assistantrepo.New(tx).AcquireAssistantAdvisoryLock(ctx, runtime.AssistantID.String()); err != nil {
		return ProvisionedAssistantWorkloadClient{}, fmt.Errorf("acquire runtime lifecycle fence: %w", err)
	}
	result, err := s.provisionAssistantRuntimeWorkloadClientLocked(ctx, tx, runtimeID, c)
	if err != nil {
		return ProvisionedAssistantWorkloadClient{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ProvisionedAssistantWorkloadClient{}, fmt.Errorf("commit assistant workload client transaction: %w", err)
	}
	return result, nil
}

func (s *Service) provisionAssistantRuntimeWorkloadClientLocked(ctx context.Context, tx pgx.Tx, runtimeID uuid.UUID, c AssistantRuntimeWorkloadClientConfig) (ProvisionedAssistantWorkloadClient, error) {
	var zero ProvisionedAssistantWorkloadClient
	runtime, err := usersessions_repo.New(tx).GetAssistantWorkloadRuntime(ctx, runtimeID)
	if err != nil {
		return zero, fmt.Errorf("resolve assistant runtime workload: %w", err)
	}
	triggerID := c.TriggerID
	if triggerID == uuid.Nil {
		roots, err := usersessions_repo.New(tx).ListAssistantWorkloadRuntimeRoots(ctx, usersessions_repo.ListAssistantWorkloadRuntimeRootsParams{ProjectID: runtime.ProjectID, OrganizationID: runtime.OrganizationID, AssistantID: runtime.AssistantID})
		if err != nil {
			return zero, fmt.Errorf("resolve assistant runtime workload: %w", err)
		}
		if len(roots) != 1 {
			return zero, errors.New("bootstrap must select an existing root trigger when the assistant has zero or multiple roots")
		}
		triggerID = roots[0]
	}
	actor := urn.NewPrincipal(urn.PrincipalTypeSystem, "assistant-runtime-bootstrap")
	authContext := new(contextvalues.AuthContext)
	authContext.ActiveOrganizationID = runtime.OrganizationID
	authContext.ProjectID = &runtime.ProjectID
	ctx = contextvalues.WithAuthenticatedActor(ctx, authContext, actor)
	return s.provisionAssistantWorkloadClientLocked(ctx, tx, ProvisionAssistantWorkloadClientParams{
		RuntimeID: runtime.ID, OrganizationID: runtime.OrganizationID, ProjectID: runtime.ProjectID, AssistantID: runtime.AssistantID,
		TriggerID: triggerID, UserSessionIssuerID: c.UserSessionIssuerID, AuthMethod: c.AuthMethod, Secret: c.Secret, JWKS: c.JWKS, JWKSURI: c.JWKSURI,
	}, actor)
}

// RevokeAssistantRuntimeWorkloadClients is the trusted switch/retirement helper.
// Use the same transaction that fences the runtime. Returned registration UUIDs
// identify cleanup targets; no secrets are returned. Live bearer admission also
// checks the runtime, so retirement is effective even before cleanup runs.
func RevokeAssistantRuntimeWorkloadClients(ctx context.Context, tx pgx.Tx, organizationID string, projectID, runtimeID uuid.UUID) ([]uuid.UUID, error) {
	if organizationID == "" || projectID == uuid.Nil || runtimeID == uuid.Nil {
		return nil, errors.New("runtime cleanup requires exact tenancy")
	}
	revoked, err := usersessions_repo.New(tx).RevokeAssistantRuntimeWorkloadClients(ctx, usersessions_repo.RevokeAssistantRuntimeWorkloadClientsParams{RuntimeID: uuid.NullUUID{UUID: runtimeID, Valid: true}, OrganizationID: conv.ToPGText(organizationID), ProjectID: projectID})
	if err != nil {
		return nil, fmt.Errorf("revoke assistant runtime workload clients: %w", err)
	}
	return revoked, nil
}

// AssistantRuntimeWorkloadBinding is resolved from the persisted OAuth session,
// never from an unverified runtime claim in a bearer token.
type AssistantRuntimeWorkloadBinding struct {
	RuntimeID            uuid.UUID
	AssistantID          uuid.UUID
	ClientRegistrationID uuid.UUID
}

// ResolveAssistantRuntimeWorkloadContext resolves an already authenticated
// workload session. Wake writers must call this in their transaction and lock
// and recheck RuntimeID before creating work; this read alone is not a fence.
func ResolveAssistantRuntimeWorkloadContext(ctx context.Context, db usersessions_repo.DBTX) (AssistantRuntimeWorkloadBinding, error) {
	var zero AssistantRuntimeWorkloadBinding
	ac, ok := contextvalues.GetAuthContext(ctx)
	actor, actorOK := contextvalues.AuthenticatedActor(ctx)
	if !ok || !actorOK || actor.Type != urn.PrincipalTypeWorkload || ac.ProjectID == nil || ac.SessionID == nil || *ac.SessionID == "" || ac.ActiveOrganizationID == "" {
		return zero, errors.New("an authenticated workload OAuth session is required")
	}
	issuer, subject, err := actor.Workload()
	if err != nil {
		return zero, fmt.Errorf("resolve assistant runtime workload: %w", err)
	}
	row, err := usersessions_repo.New(db).GetAssistantRuntimeForWorkloadSession(ctx, usersessions_repo.GetAssistantRuntimeForWorkloadSessionParams{
		Jti: *ac.SessionID, SubjectUrn: urn.NewWorkloadSubject(issuer, subject), OrganizationID: conv.ToPGText(ac.ActiveOrganizationID), ProjectID: *ac.ProjectID,
	})
	if err != nil {
		return zero, fmt.Errorf("resolve assistant runtime workload: %w", err)
	}
	return AssistantRuntimeWorkloadBinding{RuntimeID: row.RuntimeID, AssistantID: row.AssistantID, ClientRegistrationID: row.ClientRegistrationID}, nil
}
