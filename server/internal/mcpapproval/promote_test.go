package mcpapproval_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_approval"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
)

func promotePayload(id string) *gen.PromotePayload {
	return &gen.PromotePayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		RiskPolicyBypassRequestID: id,
	}
}

// seedBypassRequest plants a shadow-MCP bypass request the way a block does,
// through the risk repo.
func seedBypassRequest(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, serverURL, requesterID, note string) uuid.UUID {
	t.Helper()
	return seedBypassRequestForOrg(t, ctx, ti, ti.organizationID, projectID, serverURL, requesterID, note)
}

// seedBypassRequestForOrg is seedBypassRequest under an explicit organization,
// so tests can plant a row whose organization disagrees with the caller's.
func seedBypassRequestForOrg(t *testing.T, ctx context.Context, ti *testInstance, organizationID string, projectID uuid.UUID, serverURL, requesterID, note string) uuid.UUID {
	t.Helper()

	policyID := uuid.New()
	_, err := riskrepo.New(ti.conn).CreateRiskPolicy(ctx, riskrepo.CreateRiskPolicyParams{
		ID:             policyID,
		ProjectID:      projectID,
		OrganizationID: organizationID,
		Name:           "shadow mcp policy " + policyID.String()[:8],
		Sources:        []string{"shadow_mcp"},
		AnalyzerConfig: []byte(`{}`),
		DisabledRules:  nil,
		ScopeExempt:    pgtype.Text{},
		Enabled:        true,
		Action:         "block",
		AudienceType:   "everyone",
		AutoName:       false,
		UserMessage:    pgtype.Text{},
	})
	require.NoError(t, err)

	dimensions := []byte(`{}`)
	if serverURL != "" {
		dimensions = []byte(`{"server_url":"` + serverURL + `"}`)
	}
	targetKind := pgtype.Text{}
	targetKey := pgtype.Text{String: "policy", Valid: true}
	if serverURL != "" {
		targetKind = conv.ToPGText("shadow_mcp_server")
		targetKey = conv.ToPGText(serverURL)
	}

	row, err := riskrepo.New(ti.conn).UpsertRiskPolicyBypassRequest(ctx, riskrepo.UpsertRiskPolicyBypassRequestParams{
		ID:               uuid.New(),
		OrganizationID:   organizationID,
		ProjectID:        projectID,
		RiskPolicyID:     policyID,
		TargetKind:       targetKind,
		TargetLabel:      conv.ToPGTextEmpty(""),
		TargetKey:        targetKey,
		TargetDimensions: dimensions,
		RequesterUserID:  requesterID,
		RequesterEmail:   conv.ToPGTextEmpty(conv.Ternary(requesterID != "", requesterID+"@example.test", "")),
		Note:             conv.ToPGTextEmpty(note),
		Status:           "requested",
	})
	require.NoError(t, err)

	return row.ID
}

func TestPromote_CarriesRequesterAndLinksSource(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	bypassID := seedBypassRequest(t, ctx, ti, ti.projectID, "https://mcp.example.com/sse", "blocked-user", "hit the block during oncall")

	promoted, err := ti.service.Promote(ctx, promotePayload(bypassID.String()))
	require.NoError(t, err)

	require.Equal(t, "server_url", promoted.TargetKind)
	require.Equal(t, "https://mcp.example.com/sse", promoted.TargetRaw)
	require.Equal(t, "requested", promoted.Status)

	// The requester is the blocked employee, not the promoting admin, and
	// their justification travels with them.
	require.Equal(t, 1, promoted.RequesterCount)
	detail, err := ti.service.GetRequest(ctx, getPayload(promoted.ID))
	require.NoError(t, err)
	require.Len(t, detail.Requesters, 1)
	require.Equal(t, "blocked-user", detail.Requesters[0].UserID)
	require.NotNil(t, detail.Requesters[0].Note)
	require.Equal(t, "hit the block during oncall", *detail.Requesters[0].Note)

	// The promotion source is linked on the request row.
	row, err := ti.repo.GetApprovalRequest(ctx, repo.GetApprovalRequestParams{
		ID: uuid.MustParse(promoted.ID), ProjectID: ti.projectID,
	})
	require.NoError(t, err)
	require.True(t, row.RiskPolicyBypassRequestID.Valid)
	require.Equal(t, bypassID, row.RiskPolicyBypassRequestID.UUID)

	// The audit actor is the admin who promoted, not the blocked employee the
	// request is attributed to — the feed records who performed the API call.
	entry, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionMCPApprovalRequestCreate)
	require.NoError(t, err)
	require.Equal(t, ti.authContext.UserID, entry.ActorID)
	require.NotEqual(t, "blocked-user", entry.ActorID)
	require.Equal(t, promoted.TargetRaw, entry.SubjectDisplay)
}

// Promoting another project's bypass request is the sharpest IDOR risk in the
// workflow: the id is caller-supplied and there is no database-level pin for
// this pair, so the project-scoped resolve is the primary control.
func TestPromote_OtherProjectBypassIsNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	otherProject := createProject(t, ctx, ti.conn, ti.organizationID)
	theirs := seedBypassRequest(t, ctx, ti, otherProject, "https://theirs.example.com/sse", "their-user", "their reason")

	_, err := ti.service.Promote(ctx, promotePayload(theirs.String()))
	requireOopsCode(t, err, oops.CodeNotFound)

	// Nothing entered this project's queue.
	result, err := ti.service.ListRequests(ctx, listPayload())
	require.NoError(t, err)
	require.Empty(t, result.Requests)
}

// A row whose organization disagrees with the caller's is refused even when
// its project id matches. Application writes never produce such a row (a
// project belongs to one organization), so this seeds one directly: the org
// pin is what guarantees the admission runs under the caller's organization,
// never one read off the row.
func TestPromote_ForeignOrganizationBypassIsNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	foreignOrg := "mcpapproval-foreign-org-" + uuid.NewString()
	_, err := orgrepo.New(ti.conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          foreignOrg,
		Name:        foreignOrg,
		Slug:        foreignOrg,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	theirs := seedBypassRequestForOrg(t, ctx, ti, foreignOrg, ti.projectID, "https://theirs.example.com/sse", "their-user", "their reason")

	_, err = ti.service.Promote(ctx, promotePayload(theirs.String()))
	requireOopsCode(t, err, oops.CodeNotFound)

	// Nothing entered this project's queue.
	result, err := ti.service.ListRequests(ctx, listPayload())
	require.NoError(t, err)
	require.Empty(t, result.Requests)
}

// A whole-policy bypass names no server, so there is nothing to review.
func TestPromote_WholePolicyBypassIsRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	bypassID := seedBypassRequest(t, ctx, ti, ti.projectID, "", "someone", "let me past everything")

	_, err := ti.service.Promote(ctx, promotePayload(bypassID.String()))
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// A bypass request whose hook could not resolve a user still promotes — the
// ask is not lost — it just carries no requester attribution.
func TestPromote_UnattributedRequesterIsNotLost(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	bypassID := seedBypassRequest(t, ctx, ti, ti.projectID, "https://mcp.example.com/sse", "", "")

	promoted, err := ti.service.Promote(ctx, promotePayload(bypassID.String()))
	require.NoError(t, err)
	require.Equal(t, 0, promoted.RequesterCount)
}

// Promoting a bypass for a server someone already proactively requested joins
// the existing review rather than opening a second one.
func TestPromote_JoinsAnExistingReview(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRequest(ctx, createPayload("server_url", "https://mcp.example.com/sse", "proactive ask"))
	require.NoError(t, err)

	bypassID := seedBypassRequest(t, ctx, ti, ti.projectID, "https://mcp.example.com:443/sse", "blocked-user", "hit the block")

	promoted, err := ti.service.Promote(ctx, promotePayload(bypassID.String()))
	require.NoError(t, err)
	require.Equal(t, created.ID, promoted.ID, "one server, one review, whatever the entry point")
	require.Equal(t, 2, promoted.RequesterCount)

	// The upsert's COALESCE stamps the promotion source onto the pre-existing
	// review, so the link is not lost just because the proactive ask won the
	// race to create the row.
	row, err := ti.repo.GetApprovalRequest(ctx, repo.GetApprovalRequestParams{
		ID: uuid.MustParse(created.ID), ProjectID: ti.projectID,
	})
	require.NoError(t, err)
	require.True(t, row.RiskPolicyBypassRequestID.Valid, "the joined review carries the bypass link")
	require.Equal(t, bypassID, row.RiskPolicyBypassRequestID.UUID)
}

// A promoted bypass with no note does not erase the justification the same
// person gave proactively.
func TestPromote_EmptyBypassNoteKeepsTheEarlierJustification(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	proactive := *ti.authContext
	proactive.UserID = "blocked-user"
	proactiveCtx := contextvalues.SetAuthContext(ctx, &proactive)
	created, err := ti.service.CreateRequest(proactiveCtx, createPayload("server_url", "https://mcp.example.com/sse", "the original why"))
	require.NoError(t, err)

	bypassID := seedBypassRequest(t, ctx, ti, ti.projectID, "https://mcp.example.com/sse", "blocked-user", "")

	promoted, err := ti.service.Promote(ctx, promotePayload(bypassID.String()))
	require.NoError(t, err)
	require.Equal(t, created.ID, promoted.ID)
	require.Equal(t, 1, promoted.RequesterCount)

	detail, err := ti.service.GetRequest(ctx, getPayload(promoted.ID))
	require.NoError(t, err)
	require.Len(t, detail.Requesters, 1)
	require.NotNil(t, detail.Requesters[0].Note)
	require.Equal(t, "the original why", *detail.Requesters[0].Note)
}

func TestPromote_RequiresOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	bypassID := seedBypassRequest(t, ctx, ti, ti.projectID, "https://mcp.example.com/sse", "someone", "why")
	nonAdmin := withProject(t, ctx, ti, ti.projectID, authz.ScopeProjectWrite)

	_, err := ti.service.Promote(nonAdmin, promotePayload(bypassID.String()))
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestPromote_InvalidID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.Promote(ctx, promotePayload("not-a-uuid"))
	requireOopsCode(t, err, oops.CodeBadRequest)

	_, err = ti.service.Promote(ctx, promotePayload(uuid.NewString()))
	requireOopsCode(t, err, oops.CodeNotFound)
}
