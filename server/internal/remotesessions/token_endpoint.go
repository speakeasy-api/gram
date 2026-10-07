package remotesessions

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// maxTokenEndpointResponseBytes bounds a token endpoint response. Token
// responses are a few KiB; 64 KiB matches the other token grants.
const maxTokenEndpointResponseBytes = 64 << 10 // 64 KiB

// ErrTokenEndpointConfiguration marks a client whose stored registration
// cannot authenticate at its issuer's token endpoint.
var ErrTokenEndpointConfiguration = errors.New("remotesessions: token endpoint configuration requires remediation")

// TokenEndpoint is an authorization server's token endpoint bound to one
// registered client, its authentication and its issuer's egress, including a
// tunnel. It formats redacted: it holds the client's credentials.
type TokenEndpoint struct {
	endpoint string
	issuer   string
	issuerID uuid.UUID
	doer     httpDoer
	auth     tokenEndpointClientAuth
}

// Issuer is the authorization server's issuer identifier.
func (e *TokenEndpoint) Issuer() string { return e.issuer }

// IssuerID is the remote session issuer row the endpoint belongs to.
func (e *TokenEndpoint) IssuerID() uuid.UUID { return e.issuerID }

// ClientID is the client_id the authorization server knows the client by.
func (e *TokenEndpoint) ClientID() string { return e.auth.ClientID }

// AuthMethod is how the endpoint authenticates the client.
func (e *TokenEndpoint) AuthMethod() TokenEndpointAuthMethod { return e.auth.Method }

func (e *TokenEndpoint) String() string               { return "[token endpoint]" }
func (e *TokenEndpoint) GoString() string             { return e.String() }
func (e *TokenEndpoint) MarshalJSON() ([]byte, error) { return []byte("{}"), nil }
func (e *TokenEndpoint) LogValue() slog.Value         { return slog.StringValue(e.String()) }

// Post sends one grant authenticated as the endpoint's client and returns a
// successful response carrying an access token. Provider rejections, transport
// failures and signing failures are *TokenEndpointError; it never retries.
func (e *TokenEndpoint) Post(ctx context.Context, form url.Values) (TokenResponse, error) {
	var none TokenResponse
	req, err := newTokenEndpointRequest(ctx, e.endpoint, form, e.auth)
	if err != nil {
		if _, ok := errors.AsType[*tokenEndpointSigningError](err); ok {
			return none, &TokenEndpointError{StatusCode: 0, Code: "", Transport: false, Signing: true}
		}
		return none, fmt.Errorf("build token request: %w", err)
	}
	resp, err := e.doer.Do(req)
	if err != nil {
		return none, &TokenEndpointError{StatusCode: 0, Code: "", Transport: true, Signing: false}
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxTokenEndpointResponseBytes+1))
	if err != nil {
		return none, &TokenEndpointError{StatusCode: resp.StatusCode, Code: "", Transport: true, Signing: false}
	}
	if len(body) > maxTokenEndpointResponseBytes {
		return none, fmt.Errorf("token response exceeds %d bytes", maxTokenEndpointResponseBytes)
	}
	if resp.StatusCode/100 != 2 {
		code := ""
		if parsed, ok := oautherr.ParseTokenError(body); ok {
			code = tokenEndpointErrorCode(parsed.Code)
		}
		return none, &TokenEndpointError{StatusCode: resp.StatusCode, Code: code, Transport: false, Signing: false}
	}
	tok, err := DecodeTokenResponse(body)
	if err != nil {
		return none, err
	}
	if tok.AccessToken() == "" {
		if parsed, ok := oautherr.ParseTokenError(body); ok {
			return none, &TokenEndpointError{StatusCode: resp.StatusCode, Code: tokenEndpointErrorCode(parsed.Code), Transport: false, Signing: false}
		}
		return none, errors.New("token response has no access_token")
	}
	return tok, nil
}

// LoadClientTokenEndpoint binds a remote session client to its issuer's token
// endpoint. A missing client is pgx.ErrNoRows; a registration that cannot
// authenticate is ErrTokenEndpointConfiguration.
func (m *ChallengeManager) LoadClientTokenEndpoint(ctx context.Context, clientID uuid.UUID) (*TokenEndpoint, error) {
	client, err := repo.New(m.db).GetRemoteSessionClientWithIssuerByID(ctx, clientID)
	if err != nil {
		return nil, fmt.Errorf("read client token endpoint: %w", err)
	}
	return m.clientTokenEndpoint(client)
}

func (m *ChallengeManager) clientTokenEndpoint(client repo.GetRemoteSessionClientWithIssuerByIDRow) (*TokenEndpoint, error) {
	return m.BindTokenEndpoint(TokenEndpointRegistration{
		ClientID:                        client.ClientID,
		OrganizationID:                  client.ClientOrganizationID.String,
		ExternalClientID:                client.ExternalClientID,
		ClientSecretEncrypted:           client.ClientSecretEncrypted.String,
		TokenEndpointAuthMethod:         client.TokenEndpointAuthMethod.String,
		TokenEndpointAuthAudienceFormat: client.TokenEndpointAuthAudienceFormat.String,
		JSONWebKeySetID:                 client.JsonWebKeySetID,
		IssuerID:                        client.RemoteSessionIssuerID,
		IssuerURL:                       client.IssuerUrl,
		IssuerMetadata:                  client.IssuerMetadata,
		TokenEndpoint:                   client.TokenEndpoint.String,
		TunneledMcpServerID:             client.TunneledMcpServerID,
	})
}

// BindTokenEndpoint binds a stored registration to its issuer's token endpoint.
// A registration that cannot authenticate is ErrTokenEndpointConfiguration.
func (m *ChallengeManager) BindTokenEndpoint(reg TokenEndpointRegistration) (*TokenEndpoint, error) {
	if reg.TokenEndpoint == "" {
		return nil, ErrTokenEndpointConfiguration
	}

	secret := ""
	if reg.ClientSecretEncrypted != "" && reg.TokenEndpointAuthMethod != string(TokenEndpointAuthMethodPrivateKeyJWT) {
		var err error
		secret, err = m.enc.Decrypt(reg.ClientSecretEncrypted)
		if err != nil {
			return nil, ErrTokenEndpointConfiguration
		}
	}

	method, err := ResolveTokenEndpointAuthMethod(reg.TokenEndpointAuthMethod, secret)
	if err != nil || (method == TokenEndpointAuthMethodPrivateKeyJWT && !reg.JSONWebKeySetID.Valid) {
		return nil, ErrTokenEndpointConfiguration
	}

	audience, err := ResolveTokenEndpointAuthAudience(reg.TokenEndpointAuthAudienceFormat, clientAssertionIssuer(reg.IssuerMetadata, reg.IssuerURL), reg.TokenEndpoint)
	if err != nil && method == TokenEndpointAuthMethodPrivateKeyJWT {
		return nil, ErrTokenEndpointConfiguration
	}

	doer, err := upstreamHTTPDoer(noRedirectClient(m.policy.PooledClient()), m.tunnels, reg.TunneledMcpServerID)
	if err != nil {
		return nil, ErrTokenEndpointConfiguration
	}

	return &TokenEndpoint{
		endpoint: reg.TokenEndpoint,
		issuer:   reg.IssuerURL,
		issuerID: reg.IssuerID,
		doer:     doer,
		auth: tokenEndpointClientAuth{
			Method:                method,
			RemoteSessionClientID: reg.ClientID,
			OrganizationID:        reg.OrganizationID,
			JSONWebKeySetID:       reg.JSONWebKeySetID.UUID,
			ClientID:              reg.ExternalClientID,
			ClientSecret:          secret,
			AssertionAudience:     audience,
			AssertionSigner:       m.assertions,
		},
	}, nil
}
