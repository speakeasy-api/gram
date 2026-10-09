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

	version := runtimepolicy.DelegatedPolicyVersion(parent.DelegatedGrantsVersion.Int32)
	childPolicy, err := mcpConnectPolicy(version, parent.DelegatedGrants)
	if err != nil {
		return nil, err
	}
	childPolicyJSON, err := runtimepolicy.EncodeDelegatedPolicy(version, childPolicy)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "encode MCP credential policy").LogError(ctx, s.logger)
	}

	agent, err := agents.ResolvePrincipal(ctx, tx, authCtx.ActiveOrganizationID, actor)
	if err != nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if agents.DeriveLifecycle(agent) != agents.LifecycleActive {
		return nil, oops.C(oops.CodeForbidden)
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
		DelegatedGrantsVersion: parent.DelegatedGrantsVersion,
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

// mcpConnectPolicy keeps only the parent's mcp:connect grants and their
// exclusions, so the child can never reach more than the parent could.
func mcpConnectPolicy(version runtimepolicy.DelegatedPolicyVersion, parentRaw []byte) (runtimepolicy.DelegatedPolicy, error) {
	parentPolicy, err := runtimepolicy.DecodeDelegatedPolicy(version, parentRaw)
	if err != nil {
		return runtimepolicy.DelegatedPolicy{}, oops.C(oops.CodeUnauthorized)
	}
	var grants []authz.Grant
	hasConnect := false
	for _, grant := range parentPolicy.Effective {
		switch grant.Scope {
		case authz.ScopeMCPConnect:
			hasConnect = true
		case authz.ScopeMCPBlockedConnect:
		default:
			continue
		}
		grants = append(grants, authz.NewGrantWithSelector(grant.Scope, grant.Selector))
	}
	if !hasConnect {
		return runtimepolicy.DelegatedPolicy{}, oops.E(oops.CodeForbidden, nil, "this agent key is not allowed to connect to MCP servers")
	}
	policy, err := runtimepolicy.NewDelegatedPolicy(version, grants)
	if err != nil {
		return runtimepolicy.DelegatedPolicy{}, oops.E(oops.CodeForbidden, err, "build MCP credential policy")
	}
	return policy, nil
}

func mcpCredentialAuditMetadata(key keysrepo.ApiKey) *audit.AgentKeyCredentialMetadata {
	return &audit.AgentKeyCredentialMetadata{
		SubjectURN:             key.SubjectUrn.String,
		DelegatedGrants:        key.DelegatedGrants,
		DelegatedGrantsVersion: key.DelegatedGrantsVersion.Int32,
		ExpiresAt:              key.ExpiresAt.Time.UTC().Format(time.RFC3339Nano),
	}
}
