package mcp

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	tokenHostServerURL   = "https://app.gram.example.test"
	tokenHostPlatformURL = "https://ai.gram.example.test"
	tokenHostAuthURL     = "https://auth.gram.example.test"
	tokenHostCustomURL   = "https://mcp.customer.example.test"
	tokenHostPrivateURL  = "https://mcp.tailnet.example.test"
)

func tokenHostSession(t *testing.T, signer *sessiontokens.Signer, audience, issuer, clientID string) sessiontokens.ValidatedSession {
	t.Helper()
	token, _, err := signer.Mint(sessiontokens.MintParams{
		Subject:  urn.NewUserSubject("user-1"),
		Audience: audience,
		Issuer:   issuer,
		Lifetime: time.Hour,
		ClientID: clientID,
	})
	require.NoError(t, err)
	session, err := signer.ValidateBearer(t.Context(), token, audience, audienceNeverRevoked{})
	require.NoError(t, err)
	return session
}

func tokenHostContext(surface requestorigin.Surface, baseURL string) context.Context {
	return requestorigin.WithContext(context.Background(), requestorigin.Origin{
		Surface:          surface,
		BaseURL:          baseURL,
		OrganizationID:   "",
		NetworkIngressID: uuid.Nil,
		NetworkIdentity:  nil,
	})
}

func TestCheckPerEndpointTokenHost(t *testing.T) {
	t.Parallel()

	serverURL, err := url.Parse(tokenHostServerURL)
	require.NoError(t, err)
	signer := sessiontokens.NewSigner("token-host-test-secret")
	audience := urn.NewUserSessionIssuer(uuid.New()).String()
	endpoint := &ResolvedMcpEndpoint{RouteBase: "mcp", Slug: "endpoint-a"}

	contexts := map[string]context.Context{
		"canonical": tokenHostContext(requestorigin.SurfacePlatform, tokenHostServerURL),
		"platform":  tokenHostContext(requestorigin.SurfacePlatform, tokenHostPlatformURL),
		"custom":    tokenHostContext(requestorigin.SurfaceCustomDomain, tokenHostCustomURL),
		"private":   tokenHostContext(requestorigin.SurfacePrivateNetwork, tokenHostPrivateURL),
		"internal":  context.Background(),
	}

	cases := []struct {
		name     string
		origin   string
		baseURL  string
		issuer   string
		clientID string
		authHost bool
		accepted bool
	}{
		{name: "canonical token on canonical host", origin: "canonical", baseURL: tokenHostServerURL, issuer: tokenHostServerURL + "/mcp/endpoint-a", accepted: true},
		{name: "canonical token on platform host", origin: "platform", baseURL: tokenHostPlatformURL, issuer: tokenHostServerURL + "/mcp/endpoint-a", accepted: false},
		{name: "platform token on platform host", origin: "platform", baseURL: tokenHostPlatformURL, issuer: tokenHostPlatformURL + "/mcp/endpoint-a", accepted: true},
		{name: "platform token on canonical host", origin: "canonical", baseURL: tokenHostServerURL, issuer: tokenHostPlatformURL + "/mcp/endpoint-a", accepted: false},
		{name: "custom domain token on custom domain", origin: "custom", baseURL: tokenHostCustomURL, issuer: tokenHostCustomURL + "/mcp/endpoint-a", accepted: true},
		{name: "custom domain token on canonical host", origin: "canonical", baseURL: tokenHostServerURL, issuer: tokenHostCustomURL + "/mcp/endpoint-a", accepted: false},
		{name: "canonical token on custom domain", origin: "custom", baseURL: tokenHostCustomURL, issuer: tokenHostServerURL + "/mcp/endpoint-a", accepted: false},
		// Issuer-scoped tokens stay portable across endpoints sharing the
		// issuer on the same host: only the origin is compared.
		{name: "sibling endpoint on same host", origin: "platform", baseURL: tokenHostPlatformURL, issuer: tokenHostPlatformURL + "/x/mcp/endpoint-b", accepted: true},
		{name: "host compared case-insensitively", origin: "platform", baseURL: tokenHostPlatformURL, issuer: "https://AI.gram.example.test/mcp/endpoint-a", accepted: true},
		{name: "token without iss", origin: "custom", baseURL: tokenHostCustomURL, issuer: "", accepted: true},
		{name: "unparseable iss", origin: "canonical", baseURL: tokenHostServerURL, issuer: "endpoint-a", accepted: false},
		{name: "dashboard-minted token on another host", origin: "custom", baseURL: tokenHostCustomURL, issuer: tokenHostServerURL + "/mcp/endpoint-a", clientID: sessiontokens.FirstPartyClientID, accepted: true},
		{name: "private network ingress keeps behaviour", origin: "private", baseURL: tokenHostPrivateURL, issuer: tokenHostPlatformURL + "/mcp/endpoint-a", accepted: true},
		{name: "internal caller keeps behaviour", origin: "internal", baseURL: tokenHostServerURL, issuer: tokenHostPlatformURL + "/mcp/endpoint-a", accepted: true},
		{name: "authentication host token on canonical host", origin: "canonical", baseURL: tokenHostServerURL, issuer: tokenHostAuthURL + "/mcp/endpoint-a", authHost: true, accepted: true},
		{name: "authentication host token on platform host", origin: "platform", baseURL: tokenHostPlatformURL, issuer: tokenHostAuthURL + "/mcp/endpoint-a", authHost: true, accepted: false},
		{name: "canonical token after issuer opts into authentication host", origin: "canonical", baseURL: tokenHostServerURL, issuer: tokenHostServerURL + "/mcp/endpoint-a", authHost: true, accepted: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			service := &Service{serverURL: serverURL}
			caseEndpoint := *endpoint
			if tc.authHost {
				service.authenticationHostBaseURL = tokenHostAuthURL
				caseEndpoint.useAuthenticationHost = true
			}
			session := tokenHostSession(t, signer, audience, tc.issuer, tc.clientID)

			err := service.checkPerEndpointTokenHost(contexts[tc.origin], session, &caseEndpoint, tc.baseURL)
			if tc.accepted {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, errTokenHostMismatch)
			require.Equal(t, issuerGateReasonIssuerMismatch, issuerGateFailureReason(err))
		})
	}
}
