package risk_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	hooksrepo "github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/risk"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
)

const ownerPathSecret = "AKIAIOSFODNN7EXAMPLE"

// seedContentFinding writes a chat owned by userID ("" for no human owner), one
// message holding the secret, and a content-surface finding pointing at it.
func seedContentFinding(t *testing.T, ctx context.Context, ti *testInstance, userID string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID
	content := "please rotate " + ownerPathSecret + " before the audit"
	start := strings.Index(content, ownerPathSecret)

	repo := riskrepo.New(ti.conn)
	chatID, err := repo.CreateChatForTest(ctx, riskrepo.CreateChatForTestParams{
		ProjectID: projectID, OrganizationID: orgID, UserID: conv.ToPGTextEmpty(userID), ExternalUserID: pgtype.Text{},
	})
	require.NoError(t, err)
	msgID, err := repo.CreateChatMessageForTest(ctx, riskrepo.CreateChatMessageForTestParams{
		ChatID: chatID, ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}, Content: content,
		UserID: conv.ToPGTextEmpty(userID), ExternalUserID: pgtype.Text{},
	})
	require.NoError(t, err)

	rowID := insertUnmaskFinding(t, ti, unmaskFinding{
		orgID:         orgID,
		projectID:     projectID.String(),
		chatMessageID: msgID.String(),
		chatID:        chatID.String(),
		startPos:      int32(start),
		endPos:        int32(start + len(ownerPathSecret)),
		matchLen:      uint32(len(ownerPathSecret)),
		matchRedacted: "AKIA**************LE",
		surface:       "content",
	})
	return chatID, rowID
}

// seedOwnedAgent creates an agent owned by ownerUserID in the test organization.
// Agent names are unique per organization.
func seedOwnedAgent(t *testing.T, ctx context.Context, ti *testInstance, ownerUserID, name string) uuid.UUID {
	t.Helper()
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	_, err := orgrepo.New(ti.conn).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         conv.ToPGText(ownerUserID),
	})
	require.NoError(t, err)
	agent, err := agentrepo.New(ti.conn).CreateAgent(ctx, agentrepo.CreateAgentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		OwnerUserID:    ownerUserID,
		ProjectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		Name:           name,
	})
	require.NoError(t, err)
	return agent.ID
}

// seedAgentFinding writes an agent-only session (no human ids, agent stamped)
// with a content finding, the way agent-actor capture records one.
func seedAgentFinding(t *testing.T, ctx context.Context, ti *testInstance, agentID uuid.UUID) (uuid.UUID, uuid.UUID) {
	t.Helper()
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	chatID, rowID := seedContentFinding(t, ctx, ti, "")
	linked, err := hooksrepo.New(ti.conn).LinkChatActingAgent(ctx, hooksrepo.LinkChatActingAgentParams{
		AgentID:   uuid.NullUUID{UUID: agentID, Valid: true},
		ID:        chatID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), linked)
	return chatID, rowID
}

func requireUnmasked(t *testing.T, ctx context.Context, ti *testInstance, rowID, chatID uuid.UUID) {
	t.Helper()
	before, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionRiskResultUnmask)
	require.NoError(t, err)

	res, err := ti.service.UnmaskRiskResult(ctx, &gen.UnmaskRiskResultPayload{ID: rowID.String()})
	require.NoError(t, err)
	require.Equal(t, ownerPathSecret, res.Match)
	require.Equal(t, "available", res.RevealState)

	after, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionRiskResultUnmask)
	require.NoError(t, err)
	require.Equal(t, before+1, after, "an owner reveal is audited like a granted one")
	rec, err := audittest.LatestAuditLogByAction(t.Context(), ti.conn, audit.ActionRiskResultUnmask)
	require.NoError(t, err)
	require.Equal(t, chatID.String(), rec.SubjectSlug)
}

// A member reveals findings on their own session without chat:read.
func TestUnmaskRiskResult_OwnerPath_OwnSessionWithoutChatRead(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)

	chatID, rowID := seedContentFinding(t, ctx, ti, authCtx.UserID)

	requireUnmasked(t, authztest.WithExactGrants(t, ctx), ti, rowID, chatID)
}

// Owning the agent behind an agent-only session stands in for chat:read, and a
// suspended agent is still its owner's.
func TestUnmaskRiskResult_OwnerPath_AgentOwnerWithoutChatRead(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)

	agentID := seedOwnedAgent(t, ctx, ti, authCtx.UserID, "My Agent")
	chatID, rowID := seedAgentFinding(t, ctx, ti, agentID)
	selfCtx := authztest.WithExactGrants(t, ctx)

	requireUnmasked(t, selfCtx, ti, rowID, chatID)

	_, err := agentrepo.New(ti.conn).SuspendAgent(ctx, agentrepo.SuspendAgentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ID:             agentID,
	})
	require.NoError(t, err)
	requireUnmasked(t, selfCtx, ti, rowID, chatID)
}

// The owner path also covers the MCP evidence branch: a hosted MCP finding
// stamped with the owner's agent session reveals from stored evidence.
func TestUnmaskRiskResult_OwnerPath_AgentOwnerMCPEvidence(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	chatID, _ := seedAgentFinding(t, ctx, ti, seedOwnedAgent(t, ctx, ti, authCtx.UserID, "My MCP Agent"))
	rowID := insertUnmaskFinding(t, ti, unmaskFinding{
		orgID:            orgID,
		projectID:        projectID.String(),
		chatID:           chatID.String(),
		matchLen:         uint32(len("raw MCP credential")),
		matchRedacted:    "<redacted len=18>",
		mediationSurface: "hosted_mcp",
	})
	require.NoError(t, ti.findingEvidence.Store(ctx, risk.MCPFindingEvidenceBatch{
		OrganizationID: orgID,
		ProjectID:      projectID,
		CreatedAt:      time.Now().UTC(),
		Findings:       []risk.MCPFindingEvidence{{ID: rowID, Match: "raw MCP credential"}},
	}))

	res, err := ti.service.UnmaskRiskResult(authztest.WithExactGrants(t, ctx), &gen.UnmaskRiskResultPayload{ID: rowID.String()})
	require.NoError(t, err)
	require.Equal(t, "raw MCP credential", res.Match)
	require.Equal(t, "available", res.RevealState)
}

// Another member's session, or another member's agent session, stays closed
// without chat:read.
func TestUnmaskRiskResult_OwnerPath_OtherMemberForbidden(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	_, humanRow := seedContentFinding(t, ctx, ti, "other-user-id")
	_, agentRow := seedAgentFinding(t, ctx, ti, seedOwnedAgent(t, ctx, ti, "other-owner", "Their Agent"))
	selfCtx := authztest.WithExactGrants(t, ctx)

	for _, rowID := range []uuid.UUID{humanRow, agentRow} {
		_, err := ti.service.UnmaskRiskResult(selfCtx, &gen.UnmaskRiskResultPayload{ID: rowID.String()})
		requireOopsCode(t, err, oops.CodeForbidden)
	}
}

// Latched and deleted agents are nobody's: their sessions stop answering for
// the former owner.
func TestUnmaskRiskResult_OwnerPath_LatchedAndDeletedAgentForbidden(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	agents := agentrepo.New(ti.conn)

	latchedAgent := seedOwnedAgent(t, ctx, ti, authCtx.UserID, "Latched Agent")
	_, latchedRow := seedAgentFinding(t, ctx, ti, latchedAgent)
	_, err := agents.LatchAgentsForOwnerLossByMembership(ctx, agentrepo.LatchAgentsForOwnerLossByMembershipParams{
		OwnerReassignmentReason: conv.ToPGText("membership_loss"),
		OrganizationID:          authCtx.ActiveOrganizationID,
		OwnerUserID:             authCtx.UserID,
	})
	require.NoError(t, err)

	deletedAgent := seedOwnedAgent(t, ctx, ti, authCtx.UserID, "Deleted Agent")
	_, deletedRow := seedAgentFinding(t, ctx, ti, deletedAgent)
	_, err = agents.DeleteAgent(ctx, agentrepo.DeleteAgentParams{OrganizationID: authCtx.ActiveOrganizationID, ID: deletedAgent})
	require.NoError(t, err)

	selfCtx := authztest.WithExactGrants(t, ctx)
	for _, rowID := range []uuid.UUID{latchedRow, deletedRow} {
		_, err := ti.service.UnmaskRiskResult(selfCtx, &gen.UnmaskRiskResultPayload{ID: rowID.String()})
		requireOopsCode(t, err, oops.CodeForbidden)
	}
}

// A chat:read grant on the session still opens another member's finding.
func TestUnmaskRiskResult_OwnerPath_ChatReadGrantUnchanged(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	chatID, rowID := seedContentFinding(t, ctx, ti, "other-user-id")

	requireUnmasked(t, authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeChatRead, chatID.String())), ti, rowID, chatID)
}

// The owner path is for human members only: a caller carrying an API key
// identity keeps the plain grant check even on a session it owns, and is
// denied however the engine words that denial.
func TestUnmaskRiskResult_OwnerPath_APIKeyCallerKeepsGrantCheck(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)

	_, rowID := seedContentFinding(t, ctx, ti, authCtx.UserID)

	keyed := *authCtx
	keyed.APIKeyID = uuid.NewString()
	keyCtx := authztest.WithExactGrants(t, contextvalues.SetAuthContext(ctx, &keyed))

	res, err := ti.service.UnmaskRiskResult(keyCtx, &gen.UnmaskRiskResultPayload{ID: rowID.String()})
	require.Error(t, err)
	require.Nil(t, res)
}
