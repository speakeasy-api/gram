package risk_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk"
)

// mintRequestTargetToken mints a request link for the caller with the given
// server evidence, the way the hook deny path does.
func mintRequestTargetToken(t *testing.T, ti *testInstance, authCtx *contextvalues.AuthContext, input risk.PolicyBypassRequestTokenInput) string {
	t.Helper()

	input.OrganizationID = authCtx.ActiveOrganizationID
	input.ProjectID = authCtx.ProjectID.String()
	input.RiskPolicyID = uuid.NewString()
	token, _, err := risk.GeneratePolicyBypassRequestToken(t.Context(), ti.cacheAdapter, input, 5*time.Minute)
	require.NoError(t, err)
	return token
}

// The request page names what the link asks for. A URL loses its credentials,
// query, and fragment, and no identifier is shown for a server with a URL.
func TestGetRiskPolicyBypassRequestTarget_NamesURLServerAndTool(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	token := mintRequestTargetToken(t, ti, authCtx, risk.PolicyBypassRequestTokenInput{
		RequesterUserID:        authCtx.UserID,
		ObservedName:           new("mcp.team-notes.example.com"),
		ObservedFullURL:        new("https://user:secret@mcp.team-notes.example.com/mcp?api_key=secret#frag"),
		ObservedServerIdentity: new("team-notes"),
		ToolName:               new("search_notes"),
	})

	target, err := ti.service.GetRiskPolicyBypassRequestTarget(ctx, &gen.GetRiskPolicyBypassRequestTargetPayload{RequestToken: token})
	require.NoError(t, err)
	require.Equal(t, "mcp.team-notes.example.com", *target.ServerName)
	require.Equal(t, "https://mcp.team-notes.example.com/mcp", *target.ServerURL)
	require.Nil(t, target.ServerIdentifier)
	require.Equal(t, "search_notes", *target.ToolName)
	require.NotEmpty(t, target.ExpiresAt)
}

// A server with no URL is identified by what the agent called it. For Claude
// Code and Codex that is the configured server name, not a launch command, so
// it is returned as an identifier rather than a command.
func TestGetRiskPolicyBypassRequestTarget_ShowsIdentifierWithoutURL(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	token := mintRequestTargetToken(t, ti, authCtx, risk.PolicyBypassRequestTokenInput{
		RequesterUserID:        authCtx.UserID,
		ObservedName:           new("Local Files"),
		ObservedServerIdentity: new("local-files"),
	})

	target, err := ti.service.GetRiskPolicyBypassRequestTarget(ctx, &gen.GetRiskPolicyBypassRequestTargetPayload{RequestToken: token})
	require.NoError(t, err)
	require.Nil(t, target.ServerURL)
	require.Equal(t, "Local Files", *target.ServerName)
	require.Equal(t, "local-files", *target.ServerIdentifier)
}

// A Cursor stdio server is identified by its launch command, which can carry
// credentials. They never reach the request page.
func TestGetRiskPolicyBypassRequestTarget_RedactsCommandIdentifier(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	token := mintRequestTargetToken(t, ti, authCtx, risk.PolicyBypassRequestTokenInput{
		RequesterUserID:        authCtx.UserID,
		ObservedServerIdentity: new("API_TOKEN=secret-env npx -y local-files-mcp --api-key=secret-flag"),
	})

	target, err := ti.service.GetRiskPolicyBypassRequestTarget(ctx, &gen.GetRiskPolicyBypassRequestTargetPayload{RequestToken: token})
	require.NoError(t, err)
	require.NotNil(t, target.ServerIdentifier)
	require.Contains(t, *target.ServerIdentifier, "npx -y local-files-mcp")
	require.NotContains(t, *target.ServerIdentifier, "secret-env")
	require.NotContains(t, *target.ServerIdentifier, "secret-flag")
}

// Reading a link is bound to the caller like filing it: another user cannot
// read what someone else's link asks for.
func TestGetRiskPolicyBypassRequestTarget_RejectsOtherRequester(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	token := mintRequestTargetToken(t, ti, authCtx, risk.PolicyBypassRequestTokenInput{
		RequesterUserID: uuid.NewString(),
		ObservedFullURL: new("https://mcp.team-notes.example.com/mcp"),
	})

	target, err := ti.service.GetRiskPolicyBypassRequestTarget(ctx, &gen.GetRiskPolicyBypassRequestTargetPayload{RequestToken: token})
	require.Nil(t, target)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

// An unknown or expired link is a client error, not a server fault.
func TestGetRiskPolicyBypassRequestTarget_RejectsUnknownToken(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	target, err := ti.service.GetRiskPolicyBypassRequestTarget(ctx, &gen.GetRiskPolicyBypassRequestTargetPayload{RequestToken: "rpbr2.unknown"})
	require.Nil(t, target)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)
}
