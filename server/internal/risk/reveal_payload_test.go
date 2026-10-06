package risk_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk"
)

func TestRevealRiskResultPayload_AvailableIsAudited(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.NewGrant(authz.ScopeChatRead, authz.WildcardResource),
	)

	executionID := uuid.NewString()
	payload := `{"token":"raw MCP credential"}`
	rowID := insertUnmaskFinding(t, ti, unmaskFinding{
		orgID:            orgID,
		projectID:        projectID.String(),
		chatID:           uuid.NewString(),
		startPos:         10,
		endPos:           28,
		matchLen:         uint32(len("raw MCP credential")),
		matchRedacted:    "<redacted len=18>",
		mediationSurface: "hosted_mcp",
		executionID:      executionID,
		phase:            "request",
	})
	createdAt := time.Now().UTC()
	require.NoError(t, ti.findingEvidence.Store(ctx, risk.MCPFindingEvidenceBatch{
		OrganizationID: orgID,
		ProjectID:      projectID,
		CreatedAt:      createdAt,
		Findings:       []risk.MCPFindingEvidence{{ID: rowID, Match: "raw MCP credential"}},
		Execution:      &risk.MCPExecutionPayload{ExecutionID: executionID, Phase: "request", Payload: payload},
	}))

	before, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionRiskResultRevealPayload)
	require.NoError(t, err)

	res, err := ti.service.RevealRiskResultPayload(ctx, &gen.RevealRiskResultPayloadPayload{ID: rowID.String()})
	require.NoError(t, err)
	require.Equal(t, rowID.String(), res.ID)
	require.Equal(t, "available", res.RevealState)
	require.Equal(t, payload, res.Payload)
	require.Equal(t, "raw MCP credential", res.Payload[10:28])
	require.Equal(t, executionID, *res.ExecutionID)
	require.Equal(t, "request", *res.Phase)
	require.NotNil(t, res.ExpiresAt)
	expiresAt, err := time.Parse(time.RFC3339, *res.ExpiresAt)
	require.NoError(t, err)
	require.WithinDuration(t, createdAt.Add(90*24*time.Hour), expiresAt, time.Second)

	after, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionRiskResultRevealPayload)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
	rec, err := audittest.LatestAuditLogByAction(t.Context(), ti.conn, audit.ActionRiskResultRevealPayload)
	require.NoError(t, err)
	require.Equal(t, "risk_result", rec.SubjectType)
	require.Equal(t, rowID.String(), rec.SubjectID)
	metadata, err := audittest.DecodeAuditData(rec.Metadata)
	require.NoError(t, err)
	require.Equal(t, executionID, metadata["execution_id"])
	require.Equal(t, "request", metadata["phase"])
}

func TestRevealRiskResultPayload_EvidenceNotStoredIsNotAudited(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.NewGrant(authz.ScopeChatRead, authz.WildcardResource),
	)

	executionID := uuid.NewString()
	rowID := insertUnmaskFinding(t, ti, unmaskFinding{
		orgID:            orgID,
		projectID:        projectID.String(),
		matchLen:         5,
		matchRedacted:    "<redacted len=5>",
		mediationSurface: "hosted_mcp",
		executionID:      executionID,
		phase:            "response",
	})
	// The payload was stored but its retention window has passed.
	require.NoError(t, ti.findingEvidence.Store(ctx, risk.MCPFindingEvidenceBatch{
		OrganizationID: orgID,
		ProjectID:      projectID,
		CreatedAt:      time.Now().UTC().Add(-91 * 24 * time.Hour),
		Findings:       []risk.MCPFindingEvidence{{ID: uuid.New(), Match: "x"}},
		Execution:      &risk.MCPExecutionPayload{ExecutionID: executionID, Phase: "response", Payload: "expired"},
	}))

	before, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionRiskResultRevealPayload)
	require.NoError(t, err)

	res, err := ti.service.RevealRiskResultPayload(ctx, &gen.RevealRiskResultPayloadPayload{ID: rowID.String()})
	require.NoError(t, err)
	require.Equal(t, "evidence_not_stored", res.RevealState)
	require.Empty(t, res.Payload)
	require.Nil(t, res.ExpiresAt)
	require.Equal(t, executionID, *res.ExecutionID)

	after, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionRiskResultRevealPayload)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestRevealRiskResultPayload_RequiresWildcardChatRead(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID
	chatID := uuid.New()
	// A grant on the finding's own chat is not enough: the payload is not
	// scoped to one chat.
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.NewGrant(authz.ScopeOrgAdmin, orgID),
		authz.NewGrant(authz.ScopeChatRead, chatID.String()),
	)

	rowID := insertUnmaskFinding(t, ti, unmaskFinding{
		orgID:            orgID,
		projectID:        projectID.String(),
		chatID:           chatID.String(),
		matchLen:         5,
		matchRedacted:    "<redacted len=5>",
		mediationSurface: "hosted_mcp",
		executionID:      uuid.NewString(),
		phase:            "request",
	})

	_, err := ti.service.RevealRiskResultPayload(ctx, &gen.RevealRiskResultPayloadPayload{ID: rowID.String()})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestRevealRiskResultPayload_NonMCPFindingNotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.NewGrant(authz.ScopeChatRead, authz.WildcardResource),
	)

	rowID := insertUnmaskFinding(t, ti, unmaskFinding{
		orgID:         orgID,
		projectID:     projectID.String(),
		chatID:        uuid.NewString(),
		matchLen:      5,
		matchRedacted: "<redacted len=5>",
		surface:       "content",
	})

	_, err := ti.service.RevealRiskResultPayload(ctx, &gen.RevealRiskResultPayloadPayload{ID: rowID.String()})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}
