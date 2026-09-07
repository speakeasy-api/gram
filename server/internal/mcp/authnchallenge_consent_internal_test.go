package mcp

import (
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

const testRedirectIssuer = "https://app.example.com/mcp/my-server"

// A trimmed non-empty issuer name wins the card label; a set logo asset id
// resolves to a serveImage URL on the platform origin.
func TestIssuerCardBranding_NameAndLogo(t *testing.T) {
	t.Parallel()

	serverURL, err := url.Parse("https://app.getgram.ai")
	require.NoError(t, err)

	assetID := uuid.New()
	name := "  Corporate Okta  "
	display, logoURL := issuerCardBranding(remotesessions.Client{
		IssuerSlug:        "corp-okta",
		IssuerName:        &name,
		IssuerLogoAssetID: uuid.NullUUID{UUID: assetID, Valid: true},
	}, serverURL)
	require.Equal(t, "Corporate Okta", display)
	require.Equal(t, "https://app.getgram.ai/rpc/assets.serveImage?id="+assetID.String(), logoURL)
}

// An unset or whitespace-only name falls back to the slug, and no logo
// asset means no logo URL.
func TestIssuerCardBranding_FallsBackToSlug(t *testing.T) {
	t.Parallel()

	serverURL, err := url.Parse("https://app.getgram.ai")
	require.NoError(t, err)

	display, logoURL := issuerCardBranding(remotesessions.Client{
		IssuerSlug:        "corp-okta",
		IssuerName:        nil,
		IssuerLogoAssetID: uuid.NullUUID{},
	}, serverURL)
	require.Equal(t, "corp-okta", display)
	require.Empty(t, logoURL)

	blank := "   "
	display, logoURL = issuerCardBranding(remotesessions.Client{
		IssuerSlug:        "corp-okta",
		IssuerName:        &blank,
		IssuerLogoAssetID: uuid.NullUUID{},
	}, serverURL)
	require.Equal(t, "corp-okta", display)
	require.Empty(t, logoURL)
}

func TestBuildClientRedirect_SuccessCarriesIss(t *testing.T) {
	t.Parallel()

	got, err := buildClientRedirect(clientRedirectParams{
		RedirectURI:      "http://localhost:3000/callback",
		Issuer:           testRedirectIssuer,
		Code:             "auth-code",
		State:            "client-state",
		ErrorCode:        "",
		ErrorDescription: "",
	})
	require.NoError(t, err)

	u, err := url.Parse(got)
	require.NoError(t, err)
	require.Equal(t, testRedirectIssuer, u.Query().Get("iss"))
	require.Equal(t, "auth-code", u.Query().Get("code"))
	require.Equal(t, "client-state", u.Query().Get("state"))
}

// RFC 9207 §2 puts `iss` on error responses too, not just successful ones —
// mix-up defense is worthless if the error leg is unauthenticated.
func TestBuildClientRedirect_ErrorCarriesIss(t *testing.T) {
	t.Parallel()

	got, err := buildClientRedirect(clientRedirectParams{
		RedirectURI:      "http://localhost:3000/callback",
		Issuer:           testRedirectIssuer,
		Code:             "",
		State:            "client-state",
		ErrorCode:        "access_denied",
		ErrorDescription: "user denied consent",
	})
	require.NoError(t, err)

	u, err := url.Parse(got)
	require.NoError(t, err)
	require.Equal(t, testRedirectIssuer, u.Query().Get("iss"))
	require.Equal(t, "access_denied", u.Query().Get("error"))
	require.Equal(t, "user denied consent", u.Query().Get("error_description"))
	require.Empty(t, u.Query().Get("code"))
}

// A registered redirect_uri may legitimately carry its own query string, and a
// malicious one may carry an `iss` chosen to survive the client's comparison.
// The response value must replace it outright, leaving exactly one.
func TestBuildClientRedirect_OverwritesPreexistingIss(t *testing.T) {
	t.Parallel()

	got, err := buildClientRedirect(clientRedirectParams{
		RedirectURI:      "http://localhost:3000/callback?iss=https%3A%2F%2Fattacker.example&keep=me",
		Issuer:           testRedirectIssuer,
		Code:             "auth-code",
		State:            "",
		ErrorCode:        "",
		ErrorDescription: "",
	})
	require.NoError(t, err)

	u, err := url.Parse(got)
	require.NoError(t, err)
	require.Equal(t, []string{testRedirectIssuer}, u.Query()["iss"], "exactly one iss value must survive")
	require.Equal(t, "me", u.Query().Get("keep"), "unrelated query parameters must be preserved")
}

// A redirect_uri may legitimately carry a query string of the client's own,
// but a response parameter sitting in it must not merge into the response: a
// client that reads `code` before `error` would read this decline as a grant.
func TestBuildClientRedirect_ClearsRedirectURICodeOnError(t *testing.T) {
	t.Parallel()

	got, err := buildClientRedirect(clientRedirectParams{
		RedirectURI:      "http://localhost:3000/callback?code=INJECTED&keep=me",
		Issuer:           testRedirectIssuer,
		Code:             "",
		State:            "client-state",
		ErrorCode:        "access_denied",
		ErrorDescription: "user denied consent",
	})
	require.NoError(t, err)

	u, err := url.Parse(got)
	require.NoError(t, err)
	require.Empty(t, u.Query().Get("code"), "an error response must carry no code at all")
	require.Equal(t, "access_denied", u.Query().Get("error"))
	require.Equal(t, "me", u.Query().Get("keep"))
}

// The mirror case: a redirect_uri carrying `error` must not make a successful
// authorization read as a failure.
func TestBuildClientRedirect_ClearsRedirectURIErrorOnSuccess(t *testing.T) {
	t.Parallel()

	got, err := buildClientRedirect(clientRedirectParams{
		RedirectURI:      "http://localhost:3000/callback?error=access_denied&error_description=nope",
		Issuer:           testRedirectIssuer,
		Code:             "auth-code",
		State:            "client-state",
		ErrorCode:        "",
		ErrorDescription: "",
	})
	require.NoError(t, err)

	u, err := url.Parse(got)
	require.NoError(t, err)
	require.Equal(t, "auth-code", u.Query().Get("code"))
	require.Empty(t, u.Query().Get("error"))
	require.Empty(t, u.Query().Get("error_description"))
}

// `state` is exempt from response-owned clearing: a registered redirect_uri
// that embeds one relies on receiving it back, and RFC 6749 §3.1.2 requires
// the redirect URI's own query component to be retained.
func TestBuildClientRedirect_PreservesRedirectURIStateWhenUnset(t *testing.T) {
	t.Parallel()

	got, err := buildClientRedirect(clientRedirectParams{
		RedirectURI:      "http://localhost:3000/callback?state=route-token",
		Issuer:           testRedirectIssuer,
		Code:             "auth-code",
		State:            "",
		ErrorCode:        "",
		ErrorDescription: "",
	})
	require.NoError(t, err)

	u, err := url.Parse(got)
	require.NoError(t, err)
	require.Equal(t, "route-token", u.Query().Get("state"))
}

// When the client sent a request `state`, the response value replaces any
// embedded one outright, leaving exactly one.
func TestBuildClientRedirect_OverwritesRedirectURIStateWhenSet(t *testing.T) {
	t.Parallel()

	got, err := buildClientRedirect(clientRedirectParams{
		RedirectURI:      "http://localhost:3000/callback?state=STALE",
		Issuer:           testRedirectIssuer,
		Code:             "auth-code",
		State:            "client-state",
		ErrorCode:        "",
		ErrorDescription: "",
	})
	require.NoError(t, err)

	u, err := url.Parse(got)
	require.NoError(t, err)
	require.Equal(t, []string{"client-state"}, u.Query()["state"], "exactly one state value must survive")
}

// An issuer that is empty, or relative because it was built off a missing
// origin, produces a response a spec-compliant client rejects without
// surfacing anything. url.JoinPath reports no error for an empty base, so the
// relative case has to be caught here or not at all.
func TestBuildClientRedirect_NonAbsoluteIssuerErrors(t *testing.T) {
	t.Parallel()

	for _, issuer := range []string{"", "mcp/my-server", "/mcp/my-server", "ftp://app.example.com/mcp/s"} {
		_, err := buildClientRedirect(clientRedirectParams{
			RedirectURI:      "http://localhost:3000/callback",
			Issuer:           issuer,
			Code:             "auth-code",
			State:            "",
			ErrorCode:        "",
			ErrorDescription: "",
		})
		require.ErrorContains(t, err, "not an absolute http(s) url", "issuer %q must be rejected", issuer)
	}
}

// A redirect_uri that cannot be parsed has nowhere to put the response
// parameters, so the only safe outcome is an error. Redirecting to it anyway
// would hand the client a response carrying no iss, which it must reject.
func TestBuildClientRedirect_UnparseableRedirectURIErrors(t *testing.T) {
	t.Parallel()

	_, err := buildClientRedirect(clientRedirectParams{
		RedirectURI:      "://not-a-url",
		Issuer:           testRedirectIssuer,
		Code:             "auth-code",
		State:            "",
		ErrorCode:        "",
		ErrorDescription: "",
	})
	require.ErrorContains(t, err, "parse client redirect_uri")
}
