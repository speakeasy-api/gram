package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/crypto/bcrypt"

	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// ProvisionAssistantWorkloadClientParams selects an existing independent identity
// and an explicit authentication method. There is no default method. Secret is
// optional for secret methods (generated once when absent). PrivateKeyJWT takes
// public JWKS or JWKSURI only; private signing keys never cross this boundary.
type ProvisionAssistantWorkloadClientParams struct {
	RuntimeID           uuid.UUID
	OrganizationID      string
	ProjectID           uuid.UUID
	AssistantID         uuid.UUID
	TriggerID           uuid.UUID
	UserSessionIssuerID uuid.UUID
	AuthMethod          string
	Secret              string
	JWKS                json.RawMessage
	JWKSURI             string
}

type ProvisionedAssistantWorkloadClient struct {
	RuntimeID      uuid.UUID
	TriggerID      uuid.UUID
	RegistrationID uuid.UUID
	ClientID       string
	AuthMethod     string
	Secret         string // Returned only for secret authentication. Never log or persist plaintext.
}

// ProvisionAssistantWorkloadClient registers a credential for one existing root
// workload binding. It requires authenticated assistant:write authority, and
// atomically records the registration, incarnation-pinned binding and audit.
// Provisioning authority is not delegation: the issued token has no human actor.
func (s *Service) ProvisionAssistantWorkloadClient(ctx context.Context, p ProvisionAssistantWorkloadClientParams) (ProvisionedAssistantWorkloadClient, error) {
	var zero ProvisionedAssistantWorkloadClient
	ac, ok := contextvalues.GetAuthContext(ctx)
	actor, actorOK := contextvalues.AuthenticatedActor(ctx)
	if !ok || !actorOK || ac.ActiveOrganizationID != p.OrganizationID || p.OrganizationID == "" || p.ProjectID == uuid.Nil || p.AssistantID == uuid.Nil || p.TriggerID == uuid.Nil || p.UserSessionIssuerID == uuid.Nil || s.auditLogger == nil {
		return zero, errors.New("assistant workload provisioning requires authenticated exact-tenant authority")
	}
	ctx, err := s.authz.PrepareContext(ctx)
	if err != nil {
		return zero, fmt.Errorf("provision assistant workload client: %w", err)
	}
	if err := s.authz.Require(ctx, authz.AssistantCheck(authz.ScopeAssistantWrite, p.AssistantID.String(), p.ProjectID.String())); err != nil {
		return zero, err
	}
	return s.provisionAssistantWorkloadClient(ctx, p, actor)
}

func (s *Service) provisionAssistantWorkloadClient(ctx context.Context, p ProvisionAssistantWorkloadClientParams, actor urn.Principal) (ProvisionedAssistantWorkloadClient, error) {
	var zero ProvisionedAssistantWorkloadClient
	if p.RuntimeID == uuid.Nil || p.UserSessionIssuerID == uuid.Nil || s.auditLogger == nil {
		return zero, errors.New("an active Kit runtime and issuer are required")
	}
	runtime, err := usersessions_repo.New(s.db).GetAssistantWorkloadRuntime(ctx, p.RuntimeID)
	if err != nil {
		return zero, fmt.Errorf("provision assistant workload client: %w", err)
	}
	if runtime.ProjectID != p.ProjectID || runtime.AssistantID != p.AssistantID || runtime.OrganizationID != p.OrganizationID {
		return zero, errors.New("runtime tenancy does not match workload registration")
	}
	inline, secret, hash, err := assistantWorkloadClientAuthentication(p)
	if err != nil {
		return zero, err
	}
	identityService := new(assistantidentity.Service)
	resolved, err := identityService.Resolve(ctx, s.db, p.OrganizationID, p.ProjectID, p.AssistantID, p.TriggerID)
	if err != nil {
		return zero, fmt.Errorf("provision assistant workload client: %w", err)
	}
	if resolved.State != assistantidentity.Active || resolved.Identity == nil {
		return zero, assistantidentity.ErrInvalidIdentity
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return zero, fmt.Errorf("provision assistant workload client: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := usersessions_repo.New(tx)
	var noSecretExpiry pgtype.Timestamptz
	row, err := q.CreateUserSessionClient(ctx, usersessions_repo.CreateUserSessionClientParams{
		UserSessionIssuerID: p.UserSessionIssuerID, ClientID: uuid.NewString(), ClientSecretHash: hash,
		ClientName: "Assistant workload", RedirectUris: []string{}, TokenEndpointAuthMethod: p.AuthMethod,
		ClientSecretExpiresAt: noSecretExpiry, ClientJwks: inline, ClientJwksUri: conv.ToPGTextEmpty(p.JWKSURI),
	})
	if err != nil {
		return zero, fmt.Errorf("provision assistant workload client: %w", err)
	}
	if _, err := q.BindAssistantWorkloadClient(ctx, usersessions_repo.BindAssistantWorkloadClientParams{
		ClientID: row.ID, RuntimeID: p.RuntimeID, OrganizationID: conv.ToPGText(p.OrganizationID), ProjectID: uuid.NullUUID{UUID: p.ProjectID, Valid: true}, AssistantID: p.AssistantID, TriggerID: p.TriggerID,
	}); err != nil {
		return zero, fmt.Errorf("bind assistant workload client: %w", err)
	}
	if err := s.auditLogger.LogAssistantWorkloadOAuth(ctx, tx, audit.LogAssistantWorkloadOAuthEvent{
		SessionURN: urn.UserSession{ID: uuid.Nil}, OrganizationID: p.OrganizationID, ProjectID: p.ProjectID, Actor: actor, Runtime: p.RuntimeID, ClientURN: urn.NewUserSessionClient(row.ID), AssistantURN: urn.NewAssistant(p.AssistantID), TriggerURN: urn.NewTriggerInstance(p.TriggerID),
	}); err != nil {
		return zero, fmt.Errorf("provision assistant workload client: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return zero, fmt.Errorf("provision assistant workload client: %w", err)
	}
	return ProvisionedAssistantWorkloadClient{RuntimeID: p.RuntimeID, TriggerID: p.TriggerID, RegistrationID: row.ID, ClientID: row.ClientID, AuthMethod: p.AuthMethod, Secret: secret}, nil
}

func assistantWorkloadClientAuthentication(p ProvisionAssistantWorkloadClientParams) (json.RawMessage, string, pgtype.Text, error) {
	var absent pgtype.Text
	switch p.AuthMethod {
	case oauthwire.AuthMethodPrivateKeyJWT:
		if p.Secret != "" {
			return nil, "", absent, errors.New("private_key_jwt cannot contain a secret")
		}
		inline, err := jwks.ValidateKeySource(p.AuthMethod, p.JWKS, p.JWKSURI)
		if err != nil {
			return nil, "", absent, fmt.Errorf("validate assistant workload signing keys: %w", err)
		}
		return inline, "", absent, nil
	case oauthwire.AuthMethodClientSecretBasic, oauthwire.AuthMethodClientSecretPost:
		if (len(p.JWKS) > 0 && !jwks.IsNull(p.JWKS)) || p.JWKSURI != "" {
			return nil, "", absent, errors.New("secret authentication cannot contain signing keys")
		}
		secret := p.Secret
		var err error
		if secret == "" {
			secret, err = generateClientSecret()
			if err != nil {
				return nil, "", absent, fmt.Errorf("generate assistant workload secret: %w", err)
			}
		}
		if len(secret) < 32 || len(secret) > 72 {
			return nil, "", absent, errors.New("client secret must contain 32 to 72 bytes")
		}
		hashed, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
		if err != nil {
			return nil, "", absent, fmt.Errorf("hash assistant workload secret: %w", err)
		}
		return nil, secret, conv.ToPGText(string(hashed)), nil
	default:
		return nil, "", absent, errors.New("an explicit confidential client authentication method is required")
	}
}

type registeredAssistantWorkloadIssuer struct{ service *Service }

func (i *registeredAssistantWorkloadIssuer) IssueAssistantWorkloadToken(ctx context.Context, r AssistantWorkloadTokenRequest) (AssistantWorkloadToken, error) {
	s := i.service
	var zero AssistantWorkloadToken
	if s.auditLogger == nil || r.endpoint == nil || r.client == nil || len(r.Scopes) != 1 || r.Scopes[0] != string(authz.ScopeMCPConnect) {
		return zero, errors.New("assistant workload grant requires exactly mcp:connect")
	}
	identity, err := s.loadAssistantWorkloadClientIdentity(ctx, r.endpoint, r.ClientRegistrationID)
	if err != nil {
		return zero, err
	}
	target, ok := agentAuthorizationTarget(r.endpoint)
	if !ok {
		return zero, errors.New("resource cannot admit assistant workloads")
	}
	version := runtimepolicy.CurrentDelegatedPolicyVersion
	policy, err := encodeAgentSessionPolicy(*target, version)
	if err != nil {
		return zero, err
	}
	subject := urn.NewWorkloadSubject(identity.IssuerID, identity.Subject)
	credential, err := loadWorkloadSessionCredential(r.endpoint, subject, subject, conv.ToPGText(r.OrganizationID), policy, pgtype.Int4{Int32: int32(version), Valid: true})
	if err != nil {
		return zero, err
	}
	issuer, err := s.issuerURL(r.endpoint, r.baseURL)
	if err != nil {
		return zero, err
	}
	issuance := workloadSessionIssuance(workloadSessionTarget{userSessionIssuerID: r.IssuerID, projectID: r.ProjectID, organizationID: r.OrganizationID, issuer: issuer, audience: r.Resource}, subject, credential)
	issuance.Resource = r.Resource
	grant := workloadGrantIssuance{
		client: r.client, presented: presentedWorkload{issuerID: identity.IssuerID, subject: identity.Subject, issuerURL: ""},
		credential: credential, projectID: &r.ProjectID, endpoint: r.endpoint, session: issuance,
		beforeCommit: func(ctx context.Context, tx pgx.Tx, minted *mintedSession) error {
			// Re-read the exact binding before persistence commits. Live use repeats
			// this check and workload admission, so policy withdrawal cannot be cached.
			binding, err := usersessions_repo.New(tx).GetAssistantWorkloadClientBinding(ctx, assistantClientBindingParams(r.endpoint, r.ClientRegistrationID))
			if err != nil {
				return fmt.Errorf("provision assistant workload client: %w", err)
			}
			return s.auditLogger.LogAssistantWorkloadOAuth(ctx, tx, audit.LogAssistantWorkloadOAuthEvent{
				OrganizationID: r.OrganizationID, ProjectID: r.ProjectID, Actor: urn.NewWorkloadPrincipal(identity.IssuerID, identity.Subject),
				Runtime: binding.RuntimeID, ClientURN: urn.NewUserSessionClient(r.ClientRegistrationID), AssistantURN: urn.NewAssistant(identity.AssistantID), TriggerURN: urn.NewTriggerInstance(identity.TriggerID), SessionURN: urn.NewUserSession(minted.ID),
			})
		},
	}
	minted, err := s.mintWorkloadGrantSession(ctx, s.logger, grant)
	if err != nil {
		return zero, err
	}
	return AssistantWorkloadToken{AssistantID: identity.AssistantID, AccessToken: minted.Response.AccessToken, ExpiresAt: minted.AccessExpiresAt}, nil
}

func assistantClientBindingParams(endpoint *ResolvedMcpEndpoint, clientID uuid.UUID) usersessions_repo.GetAssistantWorkloadClientBindingParams {
	return usersessions_repo.GetAssistantWorkloadClientBindingParams{ClientID: clientID, UserSessionIssuerID: endpoint.UserSessionIssuerID, OrganizationID: conv.ToPGText(endpoint.OrganizationID), ProjectID: uuid.NullUUID{UUID: endpoint.ProjectID, Valid: true}}
}

func (s *Service) loadAssistantWorkloadClientIdentity(ctx context.Context, endpoint *ResolvedMcpEndpoint, clientID uuid.UUID) (*assistantidentity.Identity, error) {
	row, err := usersessions_repo.New(s.db).GetAssistantWorkloadClientBinding(ctx, assistantClientBindingParams(endpoint, clientID))
	if err != nil {
		return nil, fmt.Errorf("provision assistant workload client: %w", err)
	}
	identityService := new(assistantidentity.Service)
	resolved, err := identityService.Resolve(ctx, s.db, endpoint.OrganizationID, endpoint.ProjectID, row.AssistantID, row.TriggerID)
	if err != nil {
		return nil, fmt.Errorf("provision assistant workload client: %w", err)
	}
	if resolved.State != assistantidentity.Active || resolved.Identity == nil || resolved.Identity.IssuerID != row.WorkloadIssuerID || resolved.Identity.Subject != row.Subject {
		return nil, assistantidentity.ErrInvalidIdentity
	}
	return resolved.Identity, nil
}
