package keys

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/keys"
	"github.com/speakeasy-api/gram/server/internal/agentmanagement"
	"github.com/speakeasy-api/gram/server/internal/agents"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	defaultAgentAPIKeyLifetime = 90 * 24 * time.Hour
	maxAgentAPIKeyLifetime     = 365 * 24 * time.Hour
)

type preparedAgentKey struct {
	agentID    uuid.UUID
	subjectURN string
	name       string
	policy     authz.DelegatedPolicy
	policyJSON []byte
	version    authz.DelegatedPolicyVersion
	expiresAt  time.Time
	fullKey    string
	keyHash    string
	keyPrefix  string
}

func (s *Service) createAgentKey(ctx context.Context, payload *gen.CreateKeyPayload) (*gen.Key, error) {
	if err := s.requireAgentCredentialsEnabled(ctx); err != nil {
		return nil, err
	}
	if payload.AgentID == nil || payload.DelegatedGrantsVersion == nil || payload.RequestedGrants == nil || len(payload.Scopes) != 0 {
		return nil, oops.E(oops.CodeBadRequest, nil, "agent keys require agent_id, delegated policy version, requested grants, and empty transport scopes")
	}
	prepared, err := s.prepareAgentKey(ctx, *payload.AgentID, payload.Name, *payload.DelegatedGrantsVersion, payload.RequestedGrants, payload.ExpiresAt)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "access agent API keys").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	human, err := s.authorizeAgentKeyIssuance(ctx, tx, prepared.agentID, prepared.policy)
	if err != nil {
		return nil, err
	}
	created, err := s.createPreparedAgentKey(ctx, tx, human, prepared)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "save agent API key").LogError(ctx, s.logger)
	}

	return agentKeyModel(created, &prepared.fullKey)
}

func (s *Service) listAgentKeys(ctx context.Context, agentIDRaw string) (*gen.ListKeysResult, error) {
	if err := s.requireAgentCredentialsEnabled(ctx); err != nil {
		return nil, err
	}
	agentID, err := parseAgentID(agentIDRaw)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "access agent API keys").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	human, _, err := s.authorizer.RequireAgentForUpdate(ctx, tx, agentID, agentmanagement.OwnedAgentAuthorize)
	if err != nil {
		return nil, fmt.Errorf("authorize agent API key listing: %w", err)
	}
	subjectURN := agentSubjectURN(agentID)
	rows, err := repo.New(tx).ListAgentAPIKeys(ctx, repo.ListAgentAPIKeysParams{
		OrganizationID: human.Auth.ActiveOrganizationID,
		SubjectUrn:     conv.ToPGText(subjectURN),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list agent API keys").LogError(ctx, s.logger)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "finish listing agent API keys").LogError(ctx, s.logger)
	}

	keys := make([]*gen.Key, 0, len(rows))
	for _, row := range rows {
		key, err := agentKeyModel(row, nil)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "decode agent API key").LogError(ctx, s.logger)
		}
		keys = append(keys, key)
	}
	return &gen.ListKeysResult{Keys: keys}, nil
}

func (s *Service) RotateKey(ctx context.Context, payload *gen.RotateKeyPayload) (*gen.Key, error) {
	if err := s.requireAgentCredentialsEnabled(ctx); err != nil {
		return nil, err
	}
	if len(payload.Scopes) != 0 {
		return nil, oops.E(oops.CodeBadRequest, nil, "agent key rotation requires empty transport scopes")
	}
	oldKeyID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid key ID format")
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	loaded, err := s.repo.GetAPIKeyByID(ctx, repo.GetAPIKeyByIDParams{ID: oldKeyID, OrganizationID: authCtx.ActiveOrganizationID})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && loaded.Deleted) {
		return nil, oops.C(oops.CodeForbidden)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load API key for rotation").LogError(ctx, s.logger)
	}
	agentID, err := agentIDFromKey(loaded)
	if err != nil {
		return nil, oops.C(oops.CodeForbidden)
	}
	prepared, err := s.prepareAgentKey(ctx, agentID.String(), payload.Name, payload.DelegatedGrantsVersion, payload.RequestedGrants, payload.ExpiresAt)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "access agent API keys").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	human, err := s.authorizeAgentKeyIssuance(ctx, tx, agentID, prepared.policy)
	if err != nil {
		return nil, err
	}
	kr := repo.New(tx)
	oldKey, err := kr.GetAPIKeyByIDForUpdate(ctx, repo.GetAPIKeyByIDForUpdateParams{ID: oldKeyID, OrganizationID: human.Auth.ActiveOrganizationID})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (oldKey.Deleted || !oldKey.SubjectUrn.Valid || oldKey.SubjectUrn.String != prepared.subjectURN)) {
		return nil, oops.C(oops.CodeForbidden)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock agent API key for rotation").LogError(ctx, s.logger)
	}

	revoked, err := kr.DeleteAgentAPIKey(ctx, repo.DeleteAgentAPIKeyParams{ID: oldKey.ID, OrganizationID: human.Auth.ActiveOrganizationID, SubjectUrn: conv.ToPGText(prepared.subjectURN)})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "revoke rotated agent API key").LogError(ctx, s.logger)
	}
	if err := s.logAgentKeyRevoke(ctx, tx, human, revoked); err != nil {
		return nil, err
	}
	created, err := s.createPreparedAgentKey(ctx, tx, human, prepared)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "save agent API key rotation").LogError(ctx, s.logger)
	}
	return agentKeyModel(created, &prepared.fullKey)
}

func (s *Service) revokeLoadedAgentKey(ctx context.Context, tx pgx.Tx, key repo.ApiKey) error {
	agentID, err := agentIDFromKey(key)
	if err != nil {
		return oops.C(oops.CodeForbidden)
	}
	human, _, err := s.authorizer.RequireAgentForUpdate(ctx, tx, agentID, agentmanagement.OwnedAgentAuthorize)
	if err != nil {
		return fmt.Errorf("authorize agent API key revocation: %w", err)
	}
	revoked, err := repo.New(tx).DeleteAgentAPIKey(ctx, repo.DeleteAgentAPIKeyParams{ID: key.ID, OrganizationID: human.Auth.ActiveOrganizationID, SubjectUrn: conv.ToPGText(key.SubjectUrn.String)})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "revoke agent API key").LogError(ctx, s.logger)
	}
	return s.logAgentKeyRevoke(ctx, tx, human, revoked)
}

func (s *Service) prepareAgentKey(ctx context.Context, agentIDRaw, name string, versionRaw int, requestedForms []*gen.AgentPolicyGrantForm, expiryRaw *string) (preparedAgentKey, error) {
	agentID, err := parseAgentID(agentIDRaw)
	if err != nil {
		return preparedAgentKey{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return preparedAgentKey{}, oops.E(oops.CodeBadRequest, nil, "key name must not be empty")
	}
	if versionRaw != int(authz.CurrentDelegatedPolicyVersion) {
		return preparedAgentKey{}, oops.E(oops.CodeBadRequest, authz.ErrInvalidDelegatedPolicy, "unsupported delegated grants version")
	}

	requested := make([]authz.Grant, 0, len(requestedForms))
	for _, form := range requestedForms {
		if form == nil || form.Selector == nil {
			return preparedAgentKey{}, oops.E(oops.CodeBadRequest, authz.ErrInvalidDelegatedPolicy, "delegated grant and selector are required")
		}
		if form.Effect != "allow" {
			return preparedAgentKey{}, oops.E(oops.CodeBadRequest, authz.ErrInvalidDelegatedPolicy, "delegated grants must use allow effect")
		}
		requested = append(requested, authz.NewGrantWithSelector(authz.Scope(form.Scope), selectorFromForm(form.Selector)))
	}
	version := authz.DelegatedPolicyVersion(versionRaw)
	policy, err := authz.NewDelegatedPolicy(version, requested)
	if err != nil {
		return preparedAgentKey{}, oops.E(oops.CodeBadRequest, err, "invalid delegated grants")
	}
	policyJSON, err := authz.EncodeDelegatedPolicy(version, policy)
	if err != nil {
		return preparedAgentKey{}, oops.E(oops.CodeBadRequest, err, "invalid delegated grants")
	}
	expiresAt, err := parseAgentKeyExpiry(expiryRaw, time.Now().UTC())
	if err != nil {
		return preparedAgentKey{}, err
	}
	fullKey, keyHash, keyPrefix, err := auth.GenerateAPIKeyMaterial(s.keyPrefix)
	if err != nil {
		return preparedAgentKey{}, oops.E(oops.CodeUnexpected, err, "generate agent API key").LogError(ctx, s.logger)
	}

	return preparedAgentKey{
		agentID: agentID, subjectURN: agentSubjectURN(agentID), name: name, policy: policy, policyJSON: policyJSON, version: version, expiresAt: expiresAt,
		fullKey: fullKey, keyHash: keyHash, keyPrefix: keyPrefix,
	}, nil
}

func (s *Service) authorizeAgentKeyIssuance(ctx context.Context, tx pgx.Tx, agentID uuid.UUID, policy authz.DelegatedPolicy) (agentmanagement.HumanContext, error) {
	human, agent, err := s.authorizer.RequireAgentForUpdate(ctx, tx, agentID, agentmanagement.OwnedAgentAuthorize)
	if err != nil {
		return agentmanagement.HumanContext{}, fmt.Errorf("authorize agent API key issuance: %w", err)
	}
	if agents.DeriveLifecycle(agent) != agents.LifecycleActive || agent.OwnerReassignmentRequiredAt.Valid {
		return agentmanagement.HumanContext{}, oops.C(oops.CodeForbidden)
	}

	_, err = orgrepo.New(tx).LockActiveOrganizationUser(ctx, orgrepo.LockActiveOrganizationUserParams{
		UserID:         conv.ToPGText(agent.OwnerUserID),
		OrganizationID: human.Auth.ActiveOrganizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return agentmanagement.HumanContext{}, oops.C(oops.CodeForbidden)
	}
	if err != nil {
		return agentmanagement.HumanContext{}, oops.E(oops.CodeUnexpected, err, "lock active agent owner").LogError(ctx, s.logger)
	}

	agentPrincipal := urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String())
	agentPolicy, err := authz.LoadAgentPolicy(ctx, tx, human.Auth.ActiveOrganizationID, agentPrincipal)
	if err != nil {
		return agentmanagement.HumanContext{}, oops.E(oops.CodeUnexpected, err, "load live agent policy").LogError(ctx, s.logger)
	}
	ownerPrincipals, err := authz.ResolveUserPrincipals(ctx, tx, human.Auth.ActiveOrganizationID, agent.OwnerUserID)
	if errors.Is(err, authz.ErrPrincipalInvalid) || errors.Is(err, authz.ErrPrincipalNotFound) {
		return agentmanagement.HumanContext{}, oops.C(oops.CodeForbidden)
	}
	if err != nil {
		return agentmanagement.HumanContext{}, oops.E(oops.CodeUnexpected, err, "resolve live agent owner").LogError(ctx, s.logger)
	}
	ownerPolicy, err := authz.LoadGrants(ctx, tx, human.Auth.ActiveOrganizationID, ownerPrincipals)
	if err != nil {
		return agentmanagement.HumanContext{}, oops.E(oops.CodeUnexpected, err, "load live agent owner policy").LogError(ctx, s.logger)
	}

	checks := checksForDelegatedPolicy(policy)
	if len(checks) > 0 {
		if err := s.authz.EvaluateLoadedGrants(ctx, agentPolicy, checks...); err != nil {
			return agentmanagement.HumanContext{}, oops.C(oops.CodeForbidden)
		}
		if err := s.authz.EvaluateLoadedGrants(ctx, ownerPolicy, checks...); err != nil {
			return agentmanagement.HumanContext{}, oops.C(oops.CodeForbidden)
		}
	}
	return human, nil
}

func (s *Service) createPreparedAgentKey(ctx context.Context, tx pgx.Tx, human agentmanagement.HumanContext, prepared preparedAgentKey) (repo.ApiKey, error) {
	created, err := repo.New(tx).CreateAgentAPIKey(ctx, repo.CreateAgentAPIKeyParams{
		OrganizationID:         human.Auth.ActiveOrganizationID,
		CreatedByUserID:        human.Auth.UserID,
		Name:                   prepared.name,
		KeyPrefix:              prepared.keyPrefix,
		KeyHash:                prepared.keyHash,
		SubjectUrn:             conv.ToPGText(prepared.subjectURN),
		DelegatedGrants:        prepared.policyJSON,
		DelegatedGrantsVersion: pgtype.Int4{Int32: int32(prepared.version), Valid: true},
		ExpiresAt:              pgtype.Timestamptz{Time: prepared.expiresAt, InfinityModifier: pgtype.Finite, Valid: true},
	})
	if err != nil {
		return repo.ApiKey{}, oops.E(oops.CodeUnexpected, err, "create agent API key").LogError(ctx, s.logger)
	}
	expiresAt := prepared.expiresAt.Format(time.RFC3339Nano)
	if err := s.audit.LogKeyCreate(ctx, tx, audit.LogKeyCreateEvent{
		OrganizationID:   human.Auth.ActiveOrganizationID,
		ProjectID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, human.Auth.UserID),
		ActorDisplayName: human.Auth.Email,
		ActorSlug:        nil,
		KeyURN:           urn.NewAPIKey(created.ID),
		KeyName:          created.Name,
		Scopes:           []string{},
		AgentCredential: &audit.AgentKeyCredentialMetadata{
			SubjectURN: prepared.subjectURN, DelegatedGrants: prepared.policyJSON, DelegatedGrantsVersion: int32(prepared.version), ExpiresAt: expiresAt,
		},
	}); err != nil {
		return repo.ApiKey{}, oops.E(oops.CodeUnexpected, err, "add agent API key creation audit log").LogError(ctx, s.logger)
	}
	return created, nil
}

func (s *Service) logAgentKeyRevoke(ctx context.Context, tx pgx.Tx, human agentmanagement.HumanContext, key repo.ApiKey) error {
	metadata, err := agentKeyAuditMetadata(key)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "decode revoked agent API key").LogError(ctx, s.logger)
	}
	if err := s.audit.LogKeyRevoke(ctx, tx, audit.LogKeyRevokeEvent{
		OrganizationID:   human.Auth.ActiveOrganizationID,
		ProjectID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, human.Auth.UserID),
		ActorDisplayName: human.Auth.Email,
		ActorSlug:        nil,
		KeyURN:           urn.NewAPIKey(key.ID),
		KeyName:          key.Name,
		Scopes:           []string{},
		AgentCredential:  metadata,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "add agent API key revocation audit log").LogError(ctx, s.logger)
	}
	return nil
}

func agentKeyModel(key repo.ApiKey, secret *string) (*gen.Key, error) {
	if !key.SubjectUrn.Valid || !key.DelegatedGrantsVersion.Valid || !key.ExpiresAt.Valid || key.DelegatedGrants == nil {
		return nil, errors.New("incomplete agent API key profile")
	}
	version := authz.DelegatedPolicyVersion(key.DelegatedGrantsVersion.Int32)
	policy, err := authz.DecodeDelegatedPolicy(version, key.DelegatedGrants)
	if err != nil {
		return nil, fmt.Errorf("decode delegated agent API key policy: %w", err)
	}
	versionValue := int(version)
	subjectURN := key.SubjectUrn.String
	expiresAt := key.ExpiresAt.Time.Format(time.RFC3339Nano)
	var lastAccessedAt *string
	if key.LastAccessedAt.Valid {
		value := key.LastAccessedAt.Time.Format(time.RFC3339Nano)
		lastAccessedAt = &value
	}
	return &gen.Key{
		ID: key.ID.String(), OrganizationID: key.OrganizationID, ProjectID: nil, CreatedByUserID: key.CreatedByUserID, Name: key.Name, KeyPrefix: key.KeyPrefix, Key: secret, Scopes: []string{},
		SubjectUrn: &subjectURN, DelegatedGrants: delegatedPolicyModel(policy), DelegatedGrantsVersion: &versionValue, ExpiresAt: &expiresAt,
		CreatedAt: key.CreatedAt.Time.Format(time.RFC3339Nano), UpdatedAt: key.UpdatedAt.Time.Format(time.RFC3339Nano), LastAccessedAt: lastAccessedAt,
	}, nil
}

func delegatedPolicyModel(policy authz.DelegatedPolicy) *gen.AgentDelegatedPolicy {
	toModels := func(grants []authz.DelegatedPolicyGrant) []*gen.AgentDelegatedGrant {
		result := make([]*gen.AgentDelegatedGrant, 0, len(grants))
		for _, grant := range grants {
			result = append(result, &gen.AgentDelegatedGrant{Scope: string(grant.Scope), Selector: selectorModel(grant.Selector)})
		}
		return result
	}
	return &gen.AgentDelegatedPolicy{Requested: toModels(policy.Requested), Effective: toModels(policy.Effective)}
}

func agentKeyAuditMetadata(key repo.ApiKey) (*audit.AgentKeyCredentialMetadata, error) {
	if !key.SubjectUrn.Valid || !key.DelegatedGrantsVersion.Valid || !key.ExpiresAt.Valid || key.DelegatedGrants == nil {
		return nil, errors.New("incomplete agent API key profile")
	}
	if _, err := authz.DecodeDelegatedPolicy(authz.DelegatedPolicyVersion(key.DelegatedGrantsVersion.Int32), key.DelegatedGrants); err != nil {
		return nil, fmt.Errorf("validate delegated agent API key policy: %w", err)
	}
	return &audit.AgentKeyCredentialMetadata{
		SubjectURN: key.SubjectUrn.String, DelegatedGrants: key.DelegatedGrants, DelegatedGrantsVersion: key.DelegatedGrantsVersion.Int32, ExpiresAt: key.ExpiresAt.Time.Format(time.RFC3339Nano),
	}, nil
}

func checksForDelegatedPolicy(policy authz.DelegatedPolicy) []authz.Check {
	grants := policy.RuntimeGrants()
	checks := make([]authz.Check, 0, len(grants))
	for _, grant := range grants {
		dimensions := make(map[string]string, len(grant.Selector))
		for key, value := range grant.Selector {
			if key != authz.SelectorKeyResourceKind && key != authz.SelectorKeyResourceID {
				dimensions[key] = value
			}
		}
		checks = append(checks, authz.Check{
			Scope: grant.Scope, ResourceKind: grant.Selector[authz.SelectorKeyResourceKind], ResourceID: grant.Selector[authz.SelectorKeyResourceID], Dimensions: dimensions,
		})
	}
	return checks
}

func selectorFromForm(selector *gen.AgentPolicySelector) authz.Selector {
	result := authz.Selector{authz.SelectorKeyResourceKind: selector.ResourceKind, authz.SelectorKeyResourceID: selector.ResourceID}
	for key, value := range map[string]*string{
		authz.SelectorKeyDisposition: selector.Disposition, authz.SelectorKeyTool: selector.Tool, authz.SelectorKeyProjectID: selector.ProjectID,
		authz.SelectorKeyServerURL: selector.ServerURL, authz.SelectorKeyServerIdentity: selector.ServerIdentity,
	} {
		if value != nil {
			result[key] = *value
		}
	}
	return result
}

func selectorModel(selector authz.Selector) *gen.AgentPolicySelector {
	model := &gen.AgentPolicySelector{
		ResourceKind:   selector[authz.SelectorKeyResourceKind],
		ResourceID:     selector[authz.SelectorKeyResourceID],
		Disposition:    mapStringPtr(selector, authz.SelectorKeyDisposition),
		Tool:           mapStringPtr(selector, authz.SelectorKeyTool),
		ProjectID:      mapStringPtr(selector, authz.SelectorKeyProjectID),
		ServerURL:      mapStringPtr(selector, authz.SelectorKeyServerURL),
		ServerIdentity: mapStringPtr(selector, authz.SelectorKeyServerIdentity),
	}
	return model
}

func mapStringPtr(values map[string]string, key string) *string {
	value, ok := values[key]
	if !ok {
		return nil
	}
	return &value
}

func parseAgentKeyExpiry(raw *string, now time.Time) (time.Time, error) {
	if raw == nil {
		return now.Add(defaultAgentAPIKeyLifetime), nil
	}
	expiresAt, err := time.Parse(time.RFC3339, *raw)
	if err != nil {
		return time.Time{}, oops.E(oops.CodeBadRequest, err, "invalid agent API key expiry")
	}
	expiresAt = expiresAt.UTC()
	if !expiresAt.After(now) {
		return time.Time{}, oops.E(oops.CodeBadRequest, nil, "agent API key expiry must be in the future")
	}
	if expiresAt.After(now.Add(maxAgentAPIKeyLifetime)) {
		return time.Time{}, oops.E(oops.CodeBadRequest, nil, "agent API key expiry cannot exceed one year")
	}
	return expiresAt, nil
}

func parseAgentID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, oops.E(oops.CodeBadRequest, err, "invalid agent ID format")
	}
	return id, nil
}

func agentSubjectURN(agentID uuid.UUID) string {
	return urn.NewPrincipal(urn.PrincipalTypeAgent, agentID.String()).String()
}

func agentIDFromKey(key repo.ApiKey) (uuid.UUID, error) {
	if !key.SubjectUrn.Valid {
		return uuid.Nil, errors.New("API key has no principal subject")
	}
	principal, err := urn.ParsePrincipal(key.SubjectUrn.String)
	if err != nil || principal.Type != urn.PrincipalTypeAgent {
		return uuid.Nil, errors.New("API key subject is not a canonical agent principal")
	}
	agentID, err := uuid.Parse(principal.ID)
	if err != nil || agentSubjectURN(agentID) != key.SubjectUrn.String {
		return uuid.Nil, errors.New("API key subject is not a canonical agent principal")
	}
	return agentID, nil
}

func (s *Service) requireAgentCredentialsEnabled(ctx context.Context) error {
	human, err := s.authorizer.RequireHuman(ctx, s.db)
	if err != nil {
		return fmt.Errorf("require human session for agent credentials: %w", err)
	}
	evaluation, _ := feature.EvaluateFlag(ctx, s.features, feature.FlagAgentCredentialsM2, human.Auth.ActiveOrganizationID, feature.OrgProjectGroups(human.Auth.OrganizationSlug, ""))
	if evaluation != feature.EvaluationEnabled {
		return oops.C(oops.CodeNotFound)
	}
	return nil
}
