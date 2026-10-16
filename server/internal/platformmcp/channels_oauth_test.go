package platformmcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

func TestChannelsOAuthResourceIsolation(t *testing.T) {
	t.Parallel()
	base, err := url.Parse("https://api.example.test")
	require.NoError(t, err)
	service := &OAuthHTTP{baseURL: base, channelsEnabled: true}
	channels := "https://api.example.test/channels/mcp"
	for _, resource := range []string{"", "https://api.example.test/platform-mcp", channels} {
		got, err := service.requestedResource(context.Background(), resource)
		require.NoError(t, err)
		if resource == channels {
			require.Equal(t, channels, got)
		} else {
			require.Equal(t, platformResource(base), got)
		}
	}
	for _, resource := range []string{"https://other.example.test/channels/mcp", channels + "/unexpected", "https://api.example.test/channels/api"} {
		_, err := service.requestedResource(context.Background(), resource)
		require.Error(t, err)
	}
	recorder := httptest.NewRecorder()
	service.ChannelsProtectedResourceHandler().ServeHTTP(recorder, httptest.NewRequest("GET", "/.well-known/oauth-protected-resource/channels/mcp", nil))
	var metadata struct {
		Resource string   `json:"resource"`
		Servers  []string `json:"authorization_servers"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &metadata))
	require.Equal(t, channels, metadata.Resource)
	require.Equal(t, []string{platformResource(base)}, metadata.Servers)
}

func TestChannelsOAuthAdmissionDefersChannelPermissions(t *testing.T) {
	t.Parallel()
	service := newTestOAuthHTTP(t)
	service.gate = oauthTestGate{enabled: false}
	service.authorizer = allowAuthorizer{}
	principal := Principal{UserID: "user-example", OrganizationID: "org-example"}
	resource := service.baseURL.JoinPath("channels/mcp").String()
	require.NoError(t, service.gateAndAuthorizeResource(t.Context(), principal, resource))
	require.ErrorIs(t, service.gateAndAuthorize(t.Context(), principal), errPlatformMCPDisabled)
	service.authorizer = oauthTestAuthorizer{err: ErrForbidden}
	require.ErrorIs(t, service.gateAndAuthorizeResource(t.Context(), principal, resource), ErrForbidden)
}

func TestChannelsOAuthConsentLabels(t *testing.T) {
	t.Parallel()
	service := newTestOAuthHTTP(t)
	resource := service.baseURL.JoinPath("channels/mcp").String()
	challenge := oauthChallenge{Resource: resource}
	require.Equal(t, "Speakeasy Channels", service.challengeSurfaceName(challenge))
	for _, kind := range []string{"organization", "connect"} {
		recorder := httptest.NewRecorder()
		service.renderOAuthPage(recorder, oauthPageData{Kind: kind, SurfaceName: service.challengeSurfaceName(challenge), Channels: true, ClientName: "Example client", OrganizationName: "Example organization"})
		require.Contains(t, recorder.Body.String(), "Speakeasy Channels")
		require.NotContains(t, recorder.Body.String(), "Connect Platform MCP")
		require.NotContains(t, recorder.Body.String(), "requests access to Platform MCP")
		if kind == "organization" {
			// Connecting Channels is not administering an organization, and the
			// Platform MCP page must keep saying what it actually asks for.
			require.Contains(t, recorder.Body.String(), "Organization to connect")
			require.NotContains(t, recorder.Body.String(), "Organization to\n            administer")
		}
		if kind == "connect" {
			require.Contains(t, recorder.Body.String(), "Connecting does not grant those permissions")
		}
	}
	require.Equal(t, "Platform MCP", service.challengeSurfaceName(oauthChallenge{}))

	platform := httptest.NewRecorder()
	service.renderOAuthPage(platform, oauthPageData{Kind: "organization", SurfaceName: "Platform MCP", Channels: false, ClientName: "Example client", OrganizationName: "Example organization"})
	require.Contains(t, platform.Body.String(), "requests access to Platform MCP")
	require.Contains(t, platform.Body.String(), "administer")
	require.NotContains(t, platform.Body.String(), "Organization to connect")
}

// The audience a token request mints for comes from the presented credential,
// not from the request. A client that omits the optional resource parameter
// keeps the audience it was granted instead of silently falling back to the
// other one, and a client that names the wrong audience is refused.
func TestChannelsOAuthGrantResourceComesFromTheCredential(t *testing.T) {
	t.Parallel()
	base, err := url.Parse("https://api.example.test")
	require.NoError(t, err)
	service := &OAuthHTTP{baseURL: base}
	channels := channelsResource(base)
	platform := platformResource(base)

	for _, test := range []struct {
		name      string
		bound     string
		requested string
		want      string
		refused   bool
	}{
		{name: "channels credential, no resource named", bound: channels, want: channels},
		{name: "channels credential, channels named", bound: channels, requested: channels, want: channels},
		{name: "channels credential, platform named", bound: channels, requested: platform, refused: true},
		{name: "platform credential, no resource named", bound: platform, want: platform},
		{name: "platform credential, channels named", bound: platform, requested: channels, refused: true},
		{name: "credential predating binding", bound: "", want: platform},
		{name: "credential predating binding, channels named", bound: "", requested: channels, refused: true},
		{name: "unknown resource named", bound: channels, requested: "https://evil.example.test/channels/mcp", refused: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, ok := service.grantResource(credentialPayload{Resource: test.bound}, test.requested)
			if test.refused {
				require.False(t, ok)
				require.Empty(t, got)
				return
			}
			require.True(t, ok)
			require.Equal(t, test.want, got)
		})
	}
}

// The consent screen decides which product it is asking about from the bound
// audience. Deriving it from the display name instead would let a rename
// silently show an operator the wrong product.
func TestChannelsOAuthConsentDiscriminatesOnAudience(t *testing.T) {
	t.Parallel()
	service := newTestOAuthHTTP(t)
	require.True(t, service.challengeIsChannels(oauthChallenge{Resource: channelsResource(service.baseURL)}))
	require.False(t, service.challengeIsChannels(oauthChallenge{Resource: platformResource(service.baseURL)}))
	require.False(t, service.challengeIsChannels(oauthChallenge{}))
	require.False(t, service.challengeIsChannels(oauthChallenge{Resource: "https://other.example.test/channels/mcp"}))
}

// The Platform MCP feature gate is skipped for the Channels audience and for
// no other. The exemption is keyed on the resource so a Platform MCP
// authorization can never reach the ungated branch.
func TestChannelsOAuthGateExemptionIsKeyedOnTheResource(t *testing.T) {
	t.Parallel()
	service := newTestOAuthHTTP(t)
	require.True(t, service.isChannelsResource(t.Context(), channelsResource(service.baseURL)))
	require.False(t, service.isChannelsResource(t.Context(), platformResource(service.baseURL)))
	require.False(t, service.isChannelsResource(t.Context(), ""))
	require.False(t, service.isChannelsResource(t.Context(), "https://other.example.test/channels/mcp"))
}

// A Channels principal must not satisfy a check this package wrote for its own
// endpoint. surface() defaults an unset surface to Platform MCP, so the tag has
// to be set explicitly for the distinction to hold.
func TestChannelsPrincipalIsNotAPlatformMCPPrincipal(t *testing.T) {
	t.Parallel()
	principal := Principal{UserID: "user-example", OrganizationID: "org-example", ConnectionID: "connection-example", Generation: "generation-example", ClientID: "client-example"}
	channels := principal
	channels.Surface = SurfaceChannels
	platform := principal
	platform.Surface = SurfacePlatformMCP

	require.Equal(t, SurfaceChannels, channels.surface())
	require.False(t, externalRiskAudiencePrincipal(channels))
	require.True(t, externalRiskAudiencePrincipal(platform))
	require.True(t, externalRiskAudiencePrincipal(principal), "an unset surface still means the endpoint that predates the field")
}

// A credential belongs to the platform host its audience names. Deriving the
// audience from the credential must not let a code or refresh token minted on
// one host be redeemed on another, which is what callers report as an invalid
// grant rather than an unsupported target.
func TestChannelsOAuthGrantsStayOnTheirPlatformHost(t *testing.T) {
	t.Parallel()
	base, err := url.Parse("https://api.example.test")
	require.NoError(t, err)
	service := &OAuthHTTP{baseURL: base, channelsEnabled: true}
	extra := requestorigin.WithContext(t.Context(), requestorigin.Origin{Surface: requestorigin.SurfacePlatform, BaseURL: "https://ai.example.test"})

	require.True(t, service.servesResource(t.Context(), channelsResource(base)))
	require.True(t, service.servesResource(t.Context(), platformResource(base)))
	require.False(t, service.servesResource(extra, channelsResource(base)), "a channels grant from the canonical host is not redeemable on an extra platform host")
	require.False(t, service.servesResource(extra, platformResource(base)))

	extraBase, err := url.Parse("https://ai.example.test")
	require.NoError(t, err)
	require.True(t, service.servesResource(extra, channelsResource(extraBase)))

	// The functional half: a client that omits the optional resource parameter
	// on refresh keeps the Channels audience it was granted.
	got, ok := service.grantResource(credentialPayload{Resource: channelsResource(base)}, "")
	require.True(t, ok)
	require.Equal(t, channelsResource(base), got)
}

// A deployment without the Channels rollout gate serves no Channels surface at
// all: the audience is not accepted, its resource metadata route is not
// registered, and no authorization can bind to it. The three have to move
// together, or the server would advertise an audience whose endpoint answers
// 404, or accept one for an endpoint that is not served.
func TestChannelsOAuthWithholdsTheAudienceWhenTheRolloutGateIsOff(t *testing.T) {
	t.Parallel()
	base, err := url.Parse("https://api.example.test")
	require.NoError(t, err)
	service := &OAuthHTTP{baseURL: base}
	channels := channelsResource(base)

	_, err = service.requestedResource(context.Background(), channels)
	require.Error(t, err, "a disabled deployment must not bind an authorization to the channels audience")
	require.NotContains(t, err.Error(), "channels", "the refusal must not disclose the unreleased surface")

	require.False(t, service.isChannelsResource(context.Background(), channels))

	platform, err := service.requestedResource(context.Background(), platformResource(base))
	require.NoError(t, err, "withholding channels must not disturb the platform audience")
	require.Equal(t, platformResource(base), platform)

	routes := map[string]bool{}
	service.Attach(recordingMux(func(method, pattern string) { routes[method+" "+pattern] = true }))
	require.NotContains(t, routes, "GET /.well-known/oauth-protected-resource/channels/mcp")
	require.Contains(t, routes, "GET /.well-known/oauth-protected-resource/platform-mcp")

	enabled := &OAuthHTTP{baseURL: base, channelsEnabled: true}
	enabledRoutes := map[string]bool{}
	enabled.Attach(recordingMux(func(method, pattern string) { enabledRoutes[method+" "+pattern] = true }))
	require.Contains(t, enabledRoutes, "GET /.well-known/oauth-protected-resource/channels/mcp")
}

type recordingMux func(method, pattern string)

func (m recordingMux) Handle(method, pattern string, _ http.HandlerFunc) { m(method, pattern) }
