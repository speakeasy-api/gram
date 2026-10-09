package agent

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/agents"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	defaultMCPCredentialLifetime = 90 * 24 * time.Hour
	maxMCPCredentialLifetime     = 365 * 24 * time.Hour
)

// MintMcpCredential issues the caller's agent a child key holding only the
// parent's mcp:connect grants (ADR-0024). Re-minting revokes the previous one.
func (s *Service) MintMcpCredential(ctx context.Context, payload *gen.MintMcpCredentialPayload) (*gen.MintMcpCredentialResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	mode, hasMode := contextvalues.APIKeyAuthorization(ctx)
	actor, hasActor := contextvalues.AuthenticatedActor(ctx)
	if !hasMode || mode != contextvalues.APIKeyAuthorizationModePrincipal || !hasActor || actor.Type != urn.PrincipalTypeAgent {
		return nil, oops.E(oops.CodeForbidden, nil, "only an agent key can mint an MCP credential")
	}
	parentID, err := uuid.Parse(authCtx.APIKeyID)
	if err != nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	now := time.Now().UTC()
	expiresAt := now.Add(defaultMCPCredentialLifetime)
	if payload.ExpiresAt != nil {
		expiresAt, err = time.Parse(time.RFC3339, *payload.ExpiresAt)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid expires_at")
		}
		expiresAt = expiresAt.UTC()
		if !expiresAt.After(now) || expiresAt.After(now.Add(maxMCPCredentialLifetime)) {
			return nil, oops.E(oops.CodeBadRequest, nil, "expires_at must be in the future and within one year")
		}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "access agent API keys").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	kr := keysrepo.New(tx)

	// Locking the parent serializes concurrent mints so only one child survives.
	parent, err := kr.GetAPIKeyByIDForUpdate(ctx, keysrepo.GetAPIKeyByIDForUpdateParams{ID: parentID, OrganizationID: authCtx.ActiveOrganizationID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load agent API key").LogError(ctx, s.logger)
	}
	if parent.Deleted || !parent.SubjectUrn.Valid || parent.SubjectUrn.String != actor.String() ||
		!parent.DelegatedGrantsVersion.Valid || !parent.ExpiresAt.Valid || parent.DelegatedGrants == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	// A minted credential cannot mint another.
	if parent.ParentApiKeyID.Valid {
		return nil, oops.E(oops.CodeForbidden, nil, "an MCP credential cannot mint another credential")
	}
	if !parent.ExpiresAt.Time.After(now) {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if expiresAt.After(parent.ExpiresAt.Time) {
		expiresAt = parent.ExpiresAt.Time.UTC()
	}

	enrollment, err := runtimepolicy.DecodeDelegatedPolicy(runtimepolicy.DelegatedPolicyVersion(parent.DelegatedGrantsVersion.Int32), parent.DelegatedGrants)
	if err != nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	// Device sync marks an enrollment key; other agent keys cannot mint.
	if !authz.GrantsContainSelector(enrollment.RuntimeGrants(), authz.ScopeOrgDeviceAgentSync, authz.NewSelector(authz.ScopeOrgDeviceAgentSync, authCtx.ActiveOrganizationID)) {
		return nil, oops.E(oops.CodeForbidden, nil, "only an enrollment key can mint an MCP credential")
	}

	agent, err := agents.ResolvePrincipal(ctx, tx, authCtx.ActiveOrganizationID, actor)
	if err != nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if agents.DeriveLifecycle(agent) != agents.LifecycleActive {
		return nil, oops.C(oops.CodeForbidden)
	}

	childPolicy, err := s.mcpConnectPolicy(ctx, tx, authCtx.ActiveOrganizationID, actor, agent.OwnerUserID, parent.CreatedByUserID)
	if err != nil {
		return nil, err
	}
	childPolicyJSON, err := runtimepolicy.EncodeDelegatedPolicy(runtimepolicy.CurrentDelegatedPolicyVersion, childPolicy)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "encode MCP credential policy").LogError(ctx, s.logger)
	}

	revoked, err := kr.RevokeChildAPIKeys(ctx, keysrepo.RevokeChildAPIKeysParams{OrganizationID: authCtx.ActiveOrganizationID, ParentApiKeyID: uuid.NullUUID{UUID: parent.ID, Valid: true}})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "revoke previous MCP credential").LogError(ctx, s.logger)
	}
	for _, key := range revoked {
		if err := s.audit.LogKeyRevoke(ctx, tx, audit.LogKeyRevokeEvent{
			OrganizationID:   authCtx.ActiveOrganizationID,
			ProjectID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			Actor:            actor,
			ActorDisplayName: &agent.Name,
			ActorSlug:        nil,
			KeyURN:           urn.NewAPIKey(key.ID),
			KeyName:          key.Name,
			Scopes:           []string{},
			AgentCredential:  mcpCredentialAuditMetadata(key),
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "add MCP credential revocation audit log").LogError(ctx, s.logger)
		}
	}

	fullKey, keyHash, keyPrefix, err := auth.GenerateAPIKeyMaterial(s.keyPrefix)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "generate MCP credential").LogError(ctx, s.logger)
	}
	created, err := kr.CreateChildAgentAPIKey(ctx, keysrepo.CreateChildAgentAPIKeyParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		CreatedByUserID:        parent.CreatedByUserID,
		Name:                   "MCP credential " + parent.ID.String(),
		KeyPrefix:              keyPrefix,
		KeyHash:                keyHash,
		SubjectUrn:             parent.SubjectUrn,
		DelegatedGrants:        childPolicyJSON,
		DelegatedGrantsVersion: pgtype.Int4{Int32: int32(runtimepolicy.CurrentDelegatedPolicyVersion), Valid: true},
		ExpiresAt:              pgtype.Timestamptz{Time: expiresAt, InfinityModifier: pgtype.Finite, Valid: true},
		ParentApiKeyID:         uuid.NullUUID{UUID: parent.ID, Valid: true},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create MCP credential").LogError(ctx, s.logger)
	}
	if err := s.audit.LogKeyCreate(ctx, tx, audit.LogKeyCreateEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Actor:            actor,
		ActorDisplayName: &agent.Name,
		ActorSlug:        nil,
		KeyURN:           urn.NewAPIKey(created.ID),
		KeyName:          created.Name,
		Scopes:           []string{},
		AgentCredential:  mcpCredentialAuditMetadata(created),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "add MCP credential creation audit log").LogError(ctx, s.logger)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "save MCP credential").LogError(ctx, s.logger)
	}

	return &gen.MintMcpCredentialResult{
		ID:        created.ID.String(),
		Key:       fullKey,
		KeyPrefix: created.KeyPrefix,
		ExpiresAt: created.ExpiresAt.Time.UTC().Format(time.RFC3339),
	}, nil
}

// mcpConnectPolicy delegates the agent's live mcp:connect access, with its
// exclusions, bounded by its owner and by the human who authorized the
// enrollment key. The enrollment key's own grants play no part.
func (s *Service) mcpConnectPolicy(ctx context.Context, tx pgx.Tx, organizationID string, actor urn.Principal, ownerUserID, authorizerUserID string) (runtimepolicy.DelegatedPolicy, error) {
	agentPolicy, err := runtimepolicy.LoadAgentPolicy(ctx, tx, organizationID, actor)
	if err != nil {
		return runtimepolicy.DelegatedPolicy{}, oops.E(oops.CodeUnexpected, err, "load live agent policy").LogError(ctx, s.logger)
	}
	ownerPolicy, err := loadUserPolicy(ctx, tx, organizationID, ownerUserID)
	if err != nil {
		return runtimepolicy.DelegatedPolicy{}, err
	}
	authorizerPolicy, err := loadUserPolicy(ctx, tx, organizationID, authorizerUserID)
	if err != nil {
		return runtimepolicy.DelegatedPolicy{}, err
	}

	delegable, err := runtimepolicy.DelegableGrantsWithExclusions(agentPolicy, ownerPolicy, authorizerPolicy)
	if err != nil {
		return runtimepolicy.DelegatedPolicy{}, oops.E(oops.CodeUnexpected, err, "derive delegable MCP access").LogError(ctx, s.logger)
	}
	grants := make([]authz.Grant, 0, len(delegable))
	hasConnect := false
	for _, grant := range delegable {
		switch grant.Scope {
		case authz.ScopeMCPConnect:
			hasConnect = true
		case authz.ScopeMCPBlockedConnect:
		default:
			continue
		}
		grants = append(grants, grant)
	}
	if !hasConnect {
		return runtimepolicy.DelegatedPolicy{}, oops.E(oops.CodeForbidden, nil, "this agent has no MCP access to delegate")
	}
	policy, err := runtimepolicy.NewDelegatedPolicy(runtimepolicy.CurrentDelegatedPolicyVersion, grants)
	if err != nil {
		return runtimepolicy.DelegatedPolicy{}, oops.E(oops.CodeUnexpected, err, "build MCP credential policy").LogError(ctx, s.logger)
	}
	return policy, nil
}

// loadUserPolicy loads a member's live grants; a departed member delegates nothing.
func loadUserPolicy(ctx context.Context, tx pgx.Tx, organizationID, userID string) ([]authz.Grant, error) {
	principals, err := authz.ResolveUserPrincipals(ctx, tx, organizationID, userID)
	if errors.Is(err, authz.ErrPrincipalInvalid) || errors.Is(err, authz.ErrPrincipalNotFound) {
		return nil, oops.C(oops.CodeForbidden)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "resolve live member principals")
	}
	grants, err := authz.LoadGrants(ctx, tx, organizationID, principals)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load live member policy")
	}
	return grants, nil
}

func mcpCredentialAuditMetadata(key keysrepo.ApiKey) *audit.AgentKeyCredentialMetadata {
	return &audit.AgentKeyCredentialMetadata{
		SubjectURN:             key.SubjectUrn.String,
		DelegatedGrants:        key.DelegatedGrants,
		DelegatedGrantsVersion: key.DelegatedGrantsVersion.Int32,
		ExpiresAt:              key.ExpiresAt.Time.UTC().Format(time.RFC3339Nano),
	}
}
