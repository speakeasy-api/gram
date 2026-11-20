package hooks

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpapprovalrepo "github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

// seedServerDecision records a standing approve or deny decision for a
// server URL in a project, the way a review records one.
func seedServerDecision(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, projectID uuid.UUID, serverURL string, decision string, principals ...string) {
	t.Helper()
	key, ok := shadowmcp.OfficialServerKey(serverURL)
	require.True(t, ok)
	queries := mcpapprovalrepo.New(conn)
	request, err := queries.UpsertApprovalRequest(ctx, mcpapprovalrepo.UpsertApprovalRequestParams{
		OrganizationID:            organizationID,
		ProjectID:                 projectID,
		TargetKind:                "server_url",
		TargetRaw:                 serverURL,
		TargetKey:                 key,
		ArtifactRef:               conv.ToPGTextEmpty(""),
		VersionPinned:             false,
		Status:                    decision,
		RiskPolicyBypassRequestID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	if len(principals) == 0 {
		principals = []string{"user:all"}
	}
	_, err = queries.CreateApprovalDecision(ctx, mcpapprovalrepo.CreateApprovalDecisionParams{
		OrganizationID:       organizationID,
		ProjectID:            projectID,
		McpApprovalRequestID: request.ID,
		Decision:             decision,
		DecidedBy:            "user-reviewer",
		Rationale:            conv.ToPGText("decided in a test"),
		EvidenceSnapshot:     []byte(`{}`),
		EvidenceVersion:      1,
		GrantedPrincipalUrns: principals,
		McpResearchReportID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
}

// Without a blocking Shadow MCP policy a project is in the built-in
// blocklist mode: servers connect unless an administrator denied them, and a
// linked server sends agents to its official version.
func TestShadowMCPBuiltInBlocklist(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	ti.service.riskScanner = &stubResultScanner{}

	deniedURL := "https://denied.example.com/mcp"
	approvedURL := "https://approved.example.com/mcp"
	linkedURL := "https://linked.example.com/mcp"
	seedServerDecision(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID, deniedURL, "denied")
	seedServerDecision(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID, approvedURL, "approved")
	seedOfficialServer(t, ctx, ti.conn, *authCtx.ProjectID, officialServerSeed{
		Name: "Linked", RemoteURL: linkedURL, Visibility: "", EndpointSlug: "linked-" + uuid.NewString(), Unlinked: false,
	})

	policy := ti.service.lookupShadowMCPBlockingPolicy(ctx, authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), authCtx.UserID)
	require.NotNil(t, policy, "a project without a blocking policy still has the built-in blocklist")
	require.True(t, policy.IsAllowAll())
	require.Empty(t, policy.ID)

	enforce := func(serverURL string) (string, bool) {
		return ti.service.enforceShadowMCPToolAccess(ctx, authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), authCtx.UserID,
			policy, "do_thing", shadowmcp.AccessEvidence{FullURL: serverURL, URLHost: "", ServerIdentity: "server"})
	}

	for _, allowedURL := range []string{"https://unreviewed.example.com/mcp", approvedURL} {
		detail, denied := enforce(allowedURL)
		require.False(t, denied, allowedURL)
		require.Empty(t, detail, allowedURL)
	}

	detail, denied := enforce(deniedURL)
	require.True(t, denied)
	require.Contains(t, detail, "blocked by policy")

	detail, denied = enforce(linkedURL)
	require.True(t, denied)
	require.Contains(t, detail, `official version "Linked"`)

	// A denial in the built-in blocklist has no policy to request a bypass
	// of, so the message offers no request link.
	reason := ti.service.renderShadowMCPUserBlockReason(ctx, shadowMCPRequestLinkParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		ProjectID:       authCtx.ProjectID.String(),
		RequesterUserID: authCtx.UserID,
		UserMessage:     nil,
		AuditReason:     "Speakeasy blocked this tool call",
		Evidence:        shadowmcp.AccessEvidence{FullURL: deniedURL, URLHost: "", ServerIdentity: "server"},
		ToolName:        "do_thing",
		ToolInput:       nil,
		RiskPolicyID:    policy.ID,
		PolicyName:      policy.Name,
	})
	require.NotContains(t, reason, "Request access")
}

// A blocking policy that applies wins over the built-in blocklist.
func TestShadowMCPBlockingPolicyWinsOverBuiltInBlocklist(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	explicit := allowAllTestPolicy(uuid.NewString())
	ti.service.riskScanner = &stubResultScanner{shadowPolicy: explicit}

	policy := ti.service.lookupShadowMCPBlockingPolicy(ctx, authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), authCtx.UserID)
	require.Same(t, explicit, policy)
}

// A denial narrowed to people or roles blocks only them in the built-in
// blocklist; everyone else still connects.
func TestShadowMCPBuiltInBlocklistNarrowDenial(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	ti.service.riskScanner = &stubResultScanner{}

	forOthersURL := "https://others.example.com/mcp"
	forMeURL := "https://me.example.com/mcp"
	seedServerDecision(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID, forOthersURL, "denied", "user:someone-else")
	seedServerDecision(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID, forMeURL, "denied", "user:"+authCtx.UserID)

	policy := ti.service.lookupShadowMCPBlockingPolicy(ctx, authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), authCtx.UserID)
	require.NotNil(t, policy)

	enforce := func(serverURL string) bool {
		_, denied := ti.service.enforceShadowMCPToolAccess(ctx, authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), authCtx.UserID,
			policy, "do_thing", shadowmcp.AccessEvidence{FullURL: serverURL, URLHost: "", ServerIdentity: "server"})
		return denied
	}
	require.False(t, enforce(forOthersURL))
	require.True(t, enforce(forMeURL))
}

// An approval narrowed to people or roles lets only them through in the
// built-in blocklist; everyone else is blocked.
func TestShadowMCPBuiltInBlocklistNarrowApproval(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	ti.service.riskScanner = &stubResultScanner{}

	forOthersURL := "https://others-only.example.com/mcp"
	forMeURL := "https://me-only.example.com/mcp"
	seedServerDecision(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID, forOthersURL, "approved", "user:someone-else")
	seedServerDecision(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID, forMeURL, "approved", "user:"+authCtx.UserID)

	policy := ti.service.lookupShadowMCPBlockingPolicy(ctx, authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), authCtx.UserID)
	require.NotNil(t, policy)

	enforce := func(serverURL string) bool {
		_, denied := ti.service.enforceShadowMCPToolAccess(ctx, authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), authCtx.UserID,
			policy, "do_thing", shadowmcp.AccessEvidence{FullURL: serverURL, URLHost: "", ServerIdentity: "server"})
		return denied
	}
	require.True(t, enforce(forOthersURL))
	require.False(t, enforce(forMeURL))
}
