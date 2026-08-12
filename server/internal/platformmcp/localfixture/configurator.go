package localfixture

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/remotesessionprovider"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

type ClientConfigurator struct {
	config *Config
	oauth  *OAuthHTTP
	db     *pgxpool.Pool
	policy *guardian.Policy
	mu     sync.Mutex
}

var _ remotesessionprovider.ProviderClientConfigurator = (*ClientConfigurator)(nil)

func NewClientConfigurator(config *Config, oauth *OAuthHTTP, db *pgxpool.Pool, policy *guardian.Policy) *ClientConfigurator {
	return &ClientConfigurator{
		config: config,
		oauth:  oauth,
		db:     db,
		policy: policy,
		mu:     sync.Mutex{},
	}
}

func (c *ClientConfigurator) ConfigureProviderClient(ctx context.Context, request platformmcp.ProviderSetupRequest, descriptor remotesessionprovider.Descriptor) error {
	if c == nil || c.config == nil || c.oauth == nil || c.db == nil || c.policy == nil || !c.matchesDescriptor(descriptor) {
		return platformmcp.ErrProviderAdapterUnavailable
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.validateMetadata(ctx); err != nil {
		return err
	}
	issuer, err := c.ensureIssuer(ctx)
	if err != nil {
		return err
	}
	client, err := remotesessionsrepo.New(c.db).GetLocalFixtureOrganizationRemoteSessionClient(ctx, remotesessionsrepo.GetLocalFixtureOrganizationRemoteSessionClientParams{
		OrganizationID:        conv.ToPGText(request.OrganizationID),
		RemoteSessionIssuerID: issuer.ID,
	})
	switch {
	case err == nil:
		if !validFixtureClient(client) {
			return fmt.Errorf("%w: local fixture client is incompatible", platformmcp.ErrProviderAdapterUnavailable)
		}
		if !c.oauthClientRegistered(client.ClientID) {
			registration, err := c.registerClient(ctx)
			if err != nil {
				return err
			}
			client, err = c.rotateClient(ctx, client, registration.clientID)
			if err != nil {
				return err
			}
		}
		return c.attachClient(ctx, request, client.ID, issuer.ID)
	case !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("load local fixture client: %w", err)
	}

	registration, err := c.registerClient(ctx)
	if err != nil {
		return err
	}
	return c.createOrReuseClient(ctx, request, issuer.ID, registration.clientID)
}

func (c *ClientConfigurator) matchesDescriptor(descriptor remotesessionprovider.Descriptor) bool {
	return descriptor.ProviderKey == ProviderKey && descriptor.RemoteSessionIssuerID == c.config.RemoteSessionIssuerID() && descriptor.StreamableHTTPURL == c.config.RemoteURL() && descriptor.Resource == c.config.RemoteURL()
}

func (c *ClientConfigurator) ensureIssuer(ctx context.Context) (remotesessionsrepo.RemoteSessionIssuer, error) {
	tx, err := c.db.Begin(ctx)
	if err != nil {
		return remotesessionsrepo.RemoteSessionIssuer{}, fmt.Errorf("begin local fixture issuer transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := remotesessionsrepo.New(tx)
	if err := queries.LockRemoteSessionIssuerForClientBinding(ctx, c.config.RemoteSessionIssuerID()); err != nil {
		return remotesessionsrepo.RemoteSessionIssuer{}, fmt.Errorf("lock local fixture issuer: %w", err)
	}
	issuer, err := queries.GetGlobalRemoteSessionIssuerByID(ctx, c.config.RemoteSessionIssuerID())
	switch {
	case err == nil:
		if !c.validIssuer(issuer) {
			return remotesessionsrepo.RemoteSessionIssuer{}, fmt.Errorf("%w: local fixture issuer is incompatible", platformmcp.ErrProviderAdapterUnavailable)
		}
	case errors.Is(err, pgx.ErrNoRows):
		issuer, err = queries.CreateLocalFixtureGlobalRemoteSessionIssuer(ctx, remotesessionsrepo.CreateLocalFixtureGlobalRemoteSessionIssuerParams{
			ID:                                c.config.RemoteSessionIssuerID(),
			Slug:                              fixtureIssuerSlug,
			Issuer:                            c.config.OAuthIssuerURL(),
			Name:                              conv.ToPGText(fixtureServerName),
			AuthorizationEndpoint:             conv.ToPGText(c.config.OAuthAuthorizationURL()),
			TokenEndpoint:                     conv.ToPGText(c.config.OAuthTokenURL()),
			RegistrationEndpoint:              conv.ToPGText(c.config.OAuthRegistrationURL()),
			ScopesSupported:                   []string{"tools:read"},
			GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
			ResponseTypesSupported:            []string{"code"},
			TokenEndpointAuthMethodsSupported: []string{"none"},
		})
		if err != nil {
			return remotesessionsrepo.RemoteSessionIssuer{}, fmt.Errorf("create local fixture issuer: %w", err)
		}
	default:
		return remotesessionsrepo.RemoteSessionIssuer{}, fmt.Errorf("load local fixture issuer: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return remotesessionsrepo.RemoteSessionIssuer{}, fmt.Errorf("commit local fixture issuer transaction: %w", err)
	}
	return issuer, nil
}

func (c *ClientConfigurator) validIssuer(issuer remotesessionsrepo.RemoteSessionIssuer) bool {
	return issuer.ID == c.config.RemoteSessionIssuerID() && !issuer.ProjectID.Valid && !issuer.OrganizationID.Valid && issuer.Slug == fixtureIssuerSlug && issuer.Issuer == c.config.OAuthIssuerURL() && pgTextEquals(issuer.AuthorizationEndpoint, c.config.OAuthAuthorizationURL()) && pgTextEquals(issuer.TokenEndpoint, c.config.OAuthTokenURL()) && pgTextEquals(issuer.RegistrationEndpoint, c.config.OAuthRegistrationURL()) && slices.Equal(issuer.ScopesSupported, []string{"tools:read"}) && slices.Equal(issuer.GrantTypesSupported, []string{"authorization_code", "refresh_token"}) && slices.Equal(issuer.ResponseTypesSupported, []string{"code"}) && slices.Equal(issuer.TokenEndpointAuthMethodsSupported, []string{"none"}) && !issuer.ClientIDMetadataDocumentSupported && !issuer.Oidc && !issuer.Passthrough
}

func (c *ClientConfigurator) validateMetadata(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.config.OAuthAuthorizationServerMetadataURL(), nil)
	if err != nil {
		return fmt.Errorf("build local fixture metadata request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	response, err := c.policy.PooledClient().Do(request)
	if err != nil {
		return fmt.Errorf("send local fixture metadata request: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, oauthRequestMaxBytes+1))
	if err != nil {
		return fmt.Errorf("read local fixture metadata response: %w", err)
	}
	if len(body) > int(oauthRequestMaxBytes) || response.StatusCode != http.StatusOK || response.Header.Get("Cache-Control") != "no-store" {
		return fmt.Errorf("local fixture metadata is unavailable")
	}
	var metadata struct {
		Issuer                            string   `json:"issuer"`
		AuthorizationEndpoint             string   `json:"authorization_endpoint"`
		TokenEndpoint                     string   `json:"token_endpoint"`
		RegistrationEndpoint              string   `json:"registration_endpoint"`
		ResponseTypesSupported            []string `json:"response_types_supported"`
		GrantTypesSupported               []string `json:"grant_types_supported"`
		TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
		CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
		ScopesSupported                   []string `json:"scopes_supported"`
	}
	if err := json.Unmarshal(body, &metadata); err != nil || metadata.Issuer != c.config.OAuthIssuerURL() || metadata.AuthorizationEndpoint != c.config.OAuthAuthorizationURL() || metadata.TokenEndpoint != c.config.OAuthTokenURL() || metadata.RegistrationEndpoint != c.config.OAuthRegistrationURL() || !slices.Equal(metadata.ResponseTypesSupported, []string{"code"}) || !slices.Equal(metadata.GrantTypesSupported, []string{"authorization_code", "refresh_token"}) || !slices.Equal(metadata.TokenEndpointAuthMethodsSupported, []string{"none"}) || !slices.Equal(metadata.CodeChallengeMethodsSupported, []string{"S256"}) || !slices.Equal(metadata.ScopesSupported, []string{"tools:read"}) {
		return fmt.Errorf("local fixture metadata is incompatible")
	}
	return nil
}

type dcrRegistration struct {
	clientID string
}

func (c *ClientConfigurator) registerClient(ctx context.Context) (dcrRegistration, error) {
	body, err := json.Marshal(map[string]any{
		"client_name":                OAuthClientName,
		"redirect_uris":              []string{c.config.RemoteLoginCallbackURL()},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	})
	if err != nil {
		return dcrRegistration{}, fmt.Errorf("encode local fixture registration: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.OAuthRegistrationURL(), bytes.NewReader(body))
	if err != nil {
		return dcrRegistration{}, fmt.Errorf("build local fixture registration request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := c.policy.PooledClient().Do(request)
	if err != nil {
		return dcrRegistration{}, fmt.Errorf("send local fixture registration request: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, oauthRequestMaxBytes+1))
	if err != nil {
		return dcrRegistration{}, fmt.Errorf("read local fixture registration response: %w", err)
	}
	if len(responseBody) > int(oauthRequestMaxBytes) || response.StatusCode != http.StatusCreated {
		return dcrRegistration{}, fmt.Errorf("local fixture registration rejected")
	}
	var registration struct {
		ClientID                string   `json:"client_id"`
		ClientName              string   `json:"client_name"`
		RedirectURIs            []string `json:"redirect_uris"`
		GrantTypes              []string `json:"grant_types"`
		ResponseTypes           []string `json:"response_types"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
		ClientSecret            string   `json:"client_secret"`
	}
	if err := json.Unmarshal(responseBody, &registration); err != nil || registration.ClientID == "" || registration.ClientSecret != "" || registration.ClientName != OAuthClientName || !slices.Equal(registration.RedirectURIs, []string{c.config.RemoteLoginCallbackURL()}) || !slices.Equal(registration.GrantTypes, []string{"authorization_code", "refresh_token"}) || !slices.Equal(registration.ResponseTypes, []string{"code"}) || registration.TokenEndpointAuthMethod != "none" {
		return dcrRegistration{}, fmt.Errorf("local fixture registration response is incompatible")
	}
	return dcrRegistration{clientID: registration.ClientID}, nil
}

func (c *ClientConfigurator) oauthClientRegistered(clientID string) bool {
	return c.oauth.HasRegisteredClient(clientID)
}

func (c *ClientConfigurator) rotateClient(ctx context.Context, client remotesessionsrepo.RemoteSessionClient, clientID string) (remotesessionsrepo.RemoteSessionClient, error) {
	if client.ID == uuid.Nil || client.OrganizationID.String == "" || client.RemoteSessionIssuerID == uuid.Nil || clientID == "" {
		return remotesessionsrepo.RemoteSessionClient{}, platformmcp.ErrProviderAdapterUnavailable
	}
	updated, err := remotesessionsrepo.New(c.db).RotateLocalFixtureOrganizationRemoteSessionClient(ctx, remotesessionsrepo.RotateLocalFixtureOrganizationRemoteSessionClientParams{
		ClientID:              clientID,
		ID:                    client.ID,
		OrganizationID:        client.OrganizationID,
		RemoteSessionIssuerID: client.RemoteSessionIssuerID,
	})
	if err != nil {
		return remotesessionsrepo.RemoteSessionClient{}, fmt.Errorf("rotate local fixture client: %w", err)
	}
	return updated, nil
}

func (c *ClientConfigurator) createOrReuseClient(ctx context.Context, request platformmcp.ProviderSetupRequest, issuerID uuid.UUID, clientID string) error {
	tx, err := c.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin local fixture client transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := remotesessionsrepo.New(tx)
	if err := queries.LockRemoteSessionIssuerForClientBinding(ctx, issuerID); err != nil {
		return fmt.Errorf("lock local fixture client: %w", err)
	}
	if _, err := queries.GetUserSessionIssuerForProject(ctx, remotesessionsrepo.GetUserSessionIssuerForProjectParams{ID: request.UserSessionIssuerID, ProjectID: request.ProjectID}); err != nil {
		return fmt.Errorf("validate local fixture user-session issuer: %w", err)
	}
	client, err := queries.GetLocalFixtureOrganizationRemoteSessionClient(ctx, remotesessionsrepo.GetLocalFixtureOrganizationRemoteSessionClientParams{OrganizationID: conv.ToPGText(request.OrganizationID), RemoteSessionIssuerID: issuerID})
	switch {
	case err == nil:
		if !validFixtureClient(client) {
			return fmt.Errorf("%w: local fixture client is incompatible", platformmcp.ErrProviderAdapterUnavailable)
		}
	case errors.Is(err, pgx.ErrNoRows):
		client, err = queries.CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{
			ProjectID:               uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			OrganizationID:          conv.ToPGText(request.OrganizationID),
			RemoteSessionIssuerID:   issuerID,
			ClientID:                clientID,
			ClientSecretEncrypted:   pgtype.Text{String: "", Valid: false},
			ClientIDIssuedAt:        conv.ToPGTimestamptz(time.Now().UTC()),
			ClientSecretExpiresAt:   pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: 0, Valid: false},
			TokenEndpointAuthMethod: conv.ToPGText("none"),
			Scope:                   []string{"tools:read"},
			Audience:                pgtype.Text{String: "", Valid: false},
			LegacyCallbackUrl:       false,
		})
		if err != nil {
			return fmt.Errorf("create local fixture client: %w", err)
		}
	default:
		return fmt.Errorf("load local fixture client in transaction: %w", err)
	}
	if err := requireNoCompetingFixtureBinding(ctx, queries, request, issuerID, client.ID); err != nil {
		return err
	}
	if err := queries.AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessionsrepo.AttachRemoteSessionClientToUserSessionIssuerParams{RemoteSessionClientID: client.ID, UserSessionIssuerID: request.UserSessionIssuerID}); err != nil {
		return fmt.Errorf("attach local fixture client: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit local fixture client transaction: %w", err)
	}
	return nil
}

func (c *ClientConfigurator) attachClient(ctx context.Context, request platformmcp.ProviderSetupRequest, clientID, issuerID uuid.UUID) error {
	tx, err := c.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin local fixture client attachment: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := remotesessionsrepo.New(tx)
	if err := queries.LockRemoteSessionIssuerForClientBinding(ctx, issuerID); err != nil {
		return fmt.Errorf("lock local fixture client attachment: %w", err)
	}
	if _, err := queries.GetUserSessionIssuerForProject(ctx, remotesessionsrepo.GetUserSessionIssuerForProjectParams{ID: request.UserSessionIssuerID, ProjectID: request.ProjectID}); err != nil {
		return fmt.Errorf("validate local fixture user-session issuer: %w", err)
	}
	if err := requireNoCompetingFixtureBinding(ctx, queries, request, issuerID, clientID); err != nil {
		return err
	}
	if err := queries.AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessionsrepo.AttachRemoteSessionClientToUserSessionIssuerParams{RemoteSessionClientID: clientID, UserSessionIssuerID: request.UserSessionIssuerID}); err != nil {
		return fmt.Errorf("attach local fixture client: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit local fixture client attachment: %w", err)
	}
	return nil
}

func requireNoCompetingFixtureBinding(ctx context.Context, queries *remotesessionsrepo.Queries, request platformmcp.ProviderSetupRequest, issuerID, clientID uuid.UUID) error {
	bound, err := queries.ListRemoteSessionClientsByProjectIDForUserSessionIssuer(ctx, remotesessionsrepo.ListRemoteSessionClientsByProjectIDForUserSessionIssuerParams{
		ProjectID:             request.ProjectID,
		UserSessionIssuerID:   request.UserSessionIssuerID,
		OrganizationID:        conv.ToPGText(request.OrganizationID),
		RemoteSessionIssuerID: uuid.NullUUID{UUID: issuerID, Valid: true},
		Cursor:                uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		LimitValue:            2,
	})
	if err != nil {
		return fmt.Errorf("list local fixture client bindings: %w", err)
	}
	for _, candidate := range bound {
		if candidate.RemoteSessionClient.ID != clientID {
			return fmt.Errorf("%w: competing local fixture client binding", platformmcp.ErrProviderAdapterUnavailable)
		}
	}
	return nil
}

func validFixtureClient(client remotesessionsrepo.RemoteSessionClient) bool {
	return !client.ProjectID.Valid && client.OrganizationID.Valid && client.ClientID != "" && !client.ClientSecretEncrypted.Valid && pgTextEquals(client.TokenEndpointAuthMethod, "none") && slices.Equal(client.Scope, []string{"tools:read"}) && !client.Audience.Valid && !client.ClientIDMetadataUri.Valid && !client.LegacyCallbackUrl
}

func pgTextEquals(value pgtype.Text, expected string) bool {
	return value.Valid && value.String == expected
}
