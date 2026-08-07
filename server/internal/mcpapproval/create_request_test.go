package mcpapproval_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_approval"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func createPayload(kind, target, note string) *gen.CreateRequestPayload {
	payload := &gen.CreateRequestPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		TargetKind: kind, Target: target, Note: nil,
	}
	if note != "" {
		payload.Note = &note
	}

	return payload
}

func TestCreateRequest_ServerURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRequest(ctx, createPayload("server_url", "https://MCP.Example.com:443/sse?token=abc", "needed for oncall"))
	require.NoError(t, err)

	require.Equal(t, "server_url", created.TargetKind)
	require.Equal(t, "https://MCP.Example.com:443/sse?token=abc", created.TargetRaw, "the reference stays as the requester named it")
	require.Equal(t, "requested", created.Status)
	require.Equal(t, 1, created.RequesterCount)

	detail, err := ti.service.GetRequest(ctx, getPayload(created.ID))
	require.NoError(t, err)
	require.Len(t, detail.Requesters, 1)
	require.NotNil(t, detail.Requesters[0].Note)
	require.Equal(t, "needed for oncall", *detail.Requesters[0].Note)
}

// Identity resolution runs at intake, so evidence has an identity to hang off
// by the time an admin looks.
func TestCreateRequest_ResolvesIdentity(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	pinned, err := ti.service.CreateRequest(ctx, createPayload("stdio_command", "npx -y @scope/mcp-server@1.2.3", ""))
	require.NoError(t, err)
	require.NotNil(t, pinned.ArtifactRef)
	require.Equal(t, "npm:@scope/mcp-server@1.2.3", *pinned.ArtifactRef)
	require.True(t, pinned.VersionPinned)

	floating, err := ti.service.CreateRequest(ctx, createPayload("stdio_command", "npx -y @scope/other-server", ""))
	require.NoError(t, err)
	require.False(t, floating.VersionPinned)

	unresolved, err := ti.service.CreateRequest(ctx, createPayload("stdio_command", "./run-my-server --local", ""))
	require.NoError(t, err)
	require.Nil(t, unresolved.ArtifactRef, "an unidentifiable reference surfaces as unknown, not as empty")
}

// Ten people wanting the same server is one review with ten requesters — and
// URL variants that canonicalize identically converge on that one review.
func TestCreateRequest_DedupesAcrossRequesters(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	first, err := ti.service.CreateRequest(ctx, createPayload("server_url", "https://mcp.example.com/sse", "for the data team"))
	require.NoError(t, err)

	// A second user asking via a cosmetically different reference.
	other := *ti.authContext
	other.UserID = "user-two"
	otherCtx := contextvalues.SetAuthContext(ctx, &other)

	second, err := ti.service.CreateRequest(otherCtx, createPayload("server_url", "HTTPS://mcp.example.com:443/sse#section", "me too"))
	require.NoError(t, err)

	require.Equal(t, first.ID, second.ID, "one server, one review")
	require.Equal(t, 2, second.RequesterCount)
}

// The same person asking twice stays one requester, keeping the freshest
// justification without erasing the old one when the new ask carries none.
func TestCreateRequest_RepeatAskIsOneRequester(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRequest(ctx, createPayload("server_url", "https://mcp.example.com/sse", "original reason"))
	require.NoError(t, err)

	again, err := ti.service.CreateRequest(ctx, createPayload("server_url", "https://mcp.example.com/sse", ""))
	require.NoError(t, err)
	require.Equal(t, 1, again.RequesterCount)

	detail, err := ti.service.GetRequest(ctx, getPayload(created.ID))
	require.NoError(t, err)
	require.Len(t, detail.Requesters, 1)
	require.NotNil(t, detail.Requesters[0].Note)
	require.Equal(t, "original reason", *detail.Requesters[0].Note, "an empty repeat ask does not erase the justification")

	updated, err := ti.service.CreateRequest(ctx, createPayload("server_url", "https://mcp.example.com/sse", "better reason"))
	require.NoError(t, err)
	require.Equal(t, 1, updated.RequesterCount)

	detail, err = ti.service.GetRequest(ctx, getPayload(created.ID))
	require.NoError(t, err)
	require.Equal(t, "better reason", *detail.Requesters[0].Note)
}

func TestCreateRequest_RejectsBadInput(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.CreateRequest(ctx, createPayload("server_url", "not a url", ""))
	requireOopsCode(t, err, oops.CodeBadRequest)

	_, err = ti.service.CreateRequest(ctx, createPayload("registry_thing", "https://mcp.example.com", ""))
	requireOopsCode(t, err, oops.CodeBadRequest)

	_, err = ti.service.CreateRequest(ctx, createPayload("stdio_command", "   ", ""))
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// Raising a request deliberately needs no RBAC grant — the same posture as
// the block and bypass surfaces — but the feature gate still holds.
func TestCreateRequest_NeedsNoScopeButRespectsTheGate(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	ungranted := withProject(t, ctx, ti, ti.projectID)
	created, err := ti.service.CreateRequest(ungranted, createPayload("server_url", "https://mcp.example.com/sse", "no grants held"))
	require.NoError(t, err)
	require.Equal(t, 1, created.RequesterCount)

	disableMCPApproval(t, ctx, ti)
	_, err = ti.service.CreateRequest(ungranted, createPayload("server_url", "https://other.example.com/sse", ""))
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestCreateRequest_WritesAnAuditEntry(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMCPApprovalRequestCreate)
	require.NoError(t, err)

	_, err = ti.service.CreateRequest(ctx, createPayload("server_url", "https://mcp.example.com/sse", ""))
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMCPApprovalRequestCreate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
}

// A re-request reopens a denied review through this path too.
func TestCreateRequest_ReopensADeniedReview(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRequest(ctx, createPayload("server_url", "https://mcp.example.com/sse", "first ask"))
	require.NoError(t, err)

	_, err = ti.service.RecordDecision(ctx, decisionPayload(created.ID, "denied"))
	require.NoError(t, err)

	reopened, err := ti.service.CreateRequest(ctx, createPayload("server_url", "https://mcp.example.com/sse", "asking again"))
	require.NoError(t, err)
	require.Equal(t, created.ID, reopened.ID)
	require.Equal(t, "requested", reopened.Status)
}
