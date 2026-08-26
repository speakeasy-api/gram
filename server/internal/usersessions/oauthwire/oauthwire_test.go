package oauthwire_test

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/usersessions/oauthwire"
)

// canonicalResource is the shape ResolvedMcpEndpoint.RootURL produces and the
// protected-resource metadata advertises: <baseURL>/<RouteBase>/<Slug>.
const canonicalResource = "https://acme.example.com/mcp/support-bot"

func requireInvalidTarget(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var oauthErr *oauthwire.Error
	require.ErrorAs(t, err, &oauthErr, "expected *oauthwire.Error, got %T (%v)", err, err)
	require.Equal(t, "invalid_target", oauthErr.Code)
}

func TestValidateResourceIndicators_AcceptsAbsentResource(t *testing.T) {
	t.Parallel()
	// RFC 8707 §2 leaves demanding the parameter to the AS, and clients
	// predating MCP 2026-07-28 never send one.
	require.NoError(t, oauthwire.ValidateResourceIndicators(nil, canonicalResource))
}

func TestValidateResourceIndicators_AcceptsExactMatch(t *testing.T) {
	t.Parallel()
	require.NoError(t, oauthwire.ValidateResourceIndicators([]string{canonicalResource}, canonicalResource))
}

func TestValidateResourceIndicators_RejectsDifferentHost(t *testing.T) {
	t.Parallel()
	requireInvalidTarget(t, oauthwire.ValidateResourceIndicators([]string{"https://evil.example.com/mcp/support-bot"}, canonicalResource))
}

func TestValidateResourceIndicators_RejectsDifferentSlug(t *testing.T) {
	t.Parallel()
	requireInvalidTarget(t, oauthwire.ValidateResourceIndicators([]string{"https://acme.example.com/mcp/billing-bot"}, canonicalResource))
}

func TestValidateResourceIndicators_RejectsTrailingSlash(t *testing.T) {
	t.Parallel()
	// MCP 2026-07-28 tells clients to emit the no-trailing-slash form and asks
	// nothing of servers on the accepting side, so this stays a mismatch.
	requireInvalidTarget(t, oauthwire.ValidateResourceIndicators([]string{canonicalResource + "/"}, canonicalResource))
}

func TestValidateResourceIndicators_RejectsUppercaseSchemeAndHost(t *testing.T) {
	t.Parallel()
	// Pins a deliberate deviation: MCP 2026-07-28 §Canonical Server URI says
	// implementations SHOULD accept uppercase scheme and host components. This
	// surface declines, holding `resource` to the same simple string
	// comparison RFC 9207 §2.4 mandates for `iss`. Changing this is a policy
	// decision, not a bug fix.
	requireInvalidTarget(t, oauthwire.ValidateResourceIndicators([]string{"HTTPS://ACME.EXAMPLE.COM/mcp/support-bot"}, canonicalResource))
}

func TestValidateResourceIndicators_RejectsDifferentRouteBase(t *testing.T) {
	t.Parallel()
	// /mcp and /x/mcp can front the same server but mint different audiences
	// (urn:toolset vs urn:user-session-issuer), so they are never equivalent
	// resource identifiers.
	requireInvalidTarget(t, oauthwire.ValidateResourceIndicators([]string{"https://acme.example.com/x/mcp/support-bot"}, canonicalResource))
}

func TestValidateResourceIndicators_RejectsDefaultPortElision(t *testing.T) {
	t.Parallel()
	requireInvalidTarget(t, oauthwire.ValidateResourceIndicators([]string{"https://acme.example.com:443/mcp/support-bot"}, canonicalResource))
}

func TestValidateResourceIndicators_DescriptionNamesExpectedNotSubmitted(t *testing.T) {
	t.Parallel()
	// The description travels in a redirect the client renders, so the
	// submitted value must not be reflected back into it. The expected value is
	// already public in the protected-resource metadata.
	submitted := "https://evil.example.com/mcp/<script>"
	err := oauthwire.ValidateResourceIndicators([]string{submitted}, canonicalResource)

	var oauthErr *oauthwire.Error
	require.ErrorAs(t, err, &oauthErr)
	require.Contains(t, oauthErr.Description, canonicalResource)
	require.NotContains(t, oauthErr.Description, submitted)
}

func TestValidateResourceIndicators_AcceptsRepeatedMatchingValues(t *testing.T) {
	t.Parallel()
	require.NoError(t, oauthwire.ValidateResourceIndicators([]string{canonicalResource, canonicalResource}, canonicalResource))
}

func TestValidateResourceIndicators_RejectsMismatchAfterAMatch(t *testing.T) {
	t.Parallel()
	// RFC 8707 §2 lets a client repeat `resource`. Checking only the first
	// would let a matching value smuggle a mismatched one past validation.
	requireInvalidTarget(t, oauthwire.ValidateResourceIndicators(
		[]string{canonicalResource, "https://evil.example.com/mcp/support-bot"}, canonicalResource))
}

func TestValidateResourceIndicators_RejectsPresentButEmpty(t *testing.T) {
	t.Parallel()
	// `resource=` is a malformed value, not an omission: RFC 8707 §2 requires
	// any value sent to be an absolute URI. Accepting it would reintroduce the
	// silent-acceptance this validation exists to remove.
	requireInvalidTarget(t, oauthwire.ValidateResourceIndicators([]string{""}, canonicalResource))
}

func TestResourceIndicatorsFrom_DistinguishesAbsentFromEmpty(t *testing.T) {
	t.Parallel()

	require.Empty(t, oauthwire.ResourceIndicatorsFrom(url.Values{}), "absent must yield no values")

	// Present-but-empty must survive as a value rather than collapse into
	// absence: `resource=` is malformed, and validation has to see it to
	// reject it.
	require.Equal(t, []string{""}, oauthwire.ResourceIndicatorsFrom(url.Values{"resource": []string{""}}))

	require.Equal(t, []string{canonicalResource}, oauthwire.ResourceIndicatorsFrom(url.Values{"resource": []string{canonicalResource}}))

	// RFC 8707 §2 permits repeating the parameter; every value must survive
	// extraction or the later ones would go unvalidated.
	repeated := url.Values{"resource": []string{canonicalResource, "https://evil.example.com/mcp/support-bot"}}
	require.Len(t, oauthwire.ResourceIndicatorsFrom(repeated), 2)
}
