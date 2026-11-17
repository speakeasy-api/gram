package tailnetgateway_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/tailcfg"

	"github.com/speakeasy-api/gram/server/internal/netingress"
	"github.com/speakeasy-api/gram/server/internal/tailnetgateway"
)

func userResponse(login, name, pic string, tags ...string) *apitype.WhoIsResponse {
	return &apitype.WhoIsResponse{
		Node:        &tailcfg.Node{Tags: tags},
		UserProfile: &tailcfg.UserProfile{LoginName: login, DisplayName: name, ProfilePicURL: pic},
	}
}

func TestSetIdentityHeadersForUser(t *testing.T) {
	t.Parallel()
	headers := http.Header{}
	headers.Set(netingress.TailscaleUserLoginHeader, "spoofed@example.com")
	headers.Set("Tailscale-Headers-Info", "https://tailscale.com/s/serve-headers")
	headers.Set("Authorization", "Bearer keep-me")

	kind := tailnetgateway.SetIdentityHeaders(headers, userResponse("alice@example.com", "Alice Example", "https://pics.example.com/alice.png"))
	require.Equal(t, tailnetgateway.IdentityKindUser, kind)
	require.Equal(t, "alice@example.com", headers.Get(netingress.TailscaleUserLoginHeader))
	require.Equal(t, "Alice Example", headers.Get(netingress.TailscaleUserNameHeader))
	require.Equal(t, "https://pics.example.com/alice.png", headers.Get(netingress.TailscaleUserProfilePicHeader))
	require.Empty(t, headers.Get("Tailscale-Headers-Info"))
	require.Equal(t, "Bearer keep-me", headers.Get("Authorization"))

	identity, err := netingress.TailscaleIdentityParser{}.ParseIdentity(headers)
	require.NoError(t, err)
	require.Equal(t, "alice@example.com", identity.Login)
	require.Equal(t, "Alice Example", identity.Name)
}

func TestSetIdentityHeadersEncodesNonASCII(t *testing.T) {
	t.Parallel()
	headers := http.Header{}
	kind := tailnetgateway.SetIdentityHeaders(headers, userResponse("zoë@example.com", "Zoë Çelik", ""))
	require.Equal(t, tailnetgateway.IdentityKindUser, kind)
	require.Contains(t, headers.Get(netingress.TailscaleUserNameHeader), "=?utf-8?")
	require.Empty(t, headers.Get(netingress.TailscaleUserProfilePicHeader))

	identity, err := netingress.TailscaleIdentityParser{}.ParseIdentity(headers)
	require.NoError(t, err)
	require.Equal(t, "zoë@example.com", identity.Login)
	require.Equal(t, "Zoë Çelik", identity.Name)
}

func TestSetIdentityHeadersEncodesEncodedWordMarkers(t *testing.T) {
	t.Parallel()
	headers := http.Header{}
	kind := tailnetgateway.SetIdentityHeaders(headers, userResponse("alice@example.com", "Alice =?not-an-encoded-word?=", ""))
	require.Equal(t, tailnetgateway.IdentityKindUser, kind)
	require.True(t, strings.HasPrefix(headers.Get(netingress.TailscaleUserNameHeader), "=?utf-8?"), "ASCII containing =? is encoded so the parser accepts it")

	identity, err := netingress.TailscaleIdentityParser{}.ParseIdentity(headers)
	require.NoError(t, err)
	require.Equal(t, "Alice =?not-an-encoded-word?=", identity.Name)
}

func TestSetIdentityHeadersForTaggedNode(t *testing.T) {
	t.Parallel()
	headers := http.Header{}
	headers.Set(netingress.TailscaleUserLoginHeader, "spoofed@example.com")
	kind := tailnetgateway.SetIdentityHeaders(headers, userResponse("tagged-devices", "tagged-devices", "", "tag:ci"))
	require.Equal(t, tailnetgateway.IdentityKindTagged, kind)
	require.Empty(t, headers.Get(netingress.TailscaleUserLoginHeader))
	require.Empty(t, headers.Get(netingress.TailscaleUserNameHeader))

	identity, err := netingress.TailscaleIdentityParser{}.ParseIdentity(headers)
	require.NoError(t, err)
	require.Nil(t, identity)
}

func TestSetIdentityHeadersForUnknownPeer(t *testing.T) {
	t.Parallel()
	headers := http.Header{}
	headers.Set(netingress.TailscaleUserNameHeader, "spoofed")
	require.Equal(t, tailnetgateway.IdentityKindUnknown, tailnetgateway.SetIdentityHeaders(headers, nil))
	require.Empty(t, headers.Get(netingress.TailscaleUserNameHeader))
	require.Equal(t, tailnetgateway.IdentityKindUnknown, tailnetgateway.SetIdentityHeaders(headers, userResponse("", "", "")))
}
