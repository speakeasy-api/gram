package mcpapproval_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_approval"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func ensurePayload(target string) *gen.EnsureServerReviewPayload {
	return &gen.EnsureServerReviewPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		Target: target,
	}
}

func TestEnsureServerReview_OpensDossierWithoutRequester(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	dossier, err := ti.service.EnsureServerReview(ctx, ensurePayload("https://dossier.example.com/mcp"))
	require.NoError(t, err)

	require.Equal(t, "unreviewed", dossier.Status)
	require.Equal(t, "server_url", dossier.TargetKind)
	require.Equal(t, 0, dossier.RequesterCount)

	detail, err := ti.service.GetRequest(ctx, getPayload(dossier.ID))
	require.NoError(t, err)
	require.Empty(t, detail.Requesters)
	require.Empty(t, detail.Decisions)

	// Re-ensuring resolves the same dossier rather than opening a second.
	again, err := ti.service.EnsureServerReview(ctx, ensurePayload("https://dossier.example.com/mcp"))
	require.NoError(t, err)
	require.Equal(t, dossier.ID, again.ID)
	require.Equal(t, "unreviewed", again.Status)
}

func TestEnsureServerReview_StaysOutOfTheQueue(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.EnsureServerReview(ctx, ensurePayload("https://quiet.example.com/mcp"))
	require.NoError(t, err)

	byDefault, err := ti.service.ListRequests(ctx, listPayload())
	require.NoError(t, err)
	require.Empty(t, byDefault.Requests)

	explicit := listPayload()
	explicit.Status = conv.PtrEmpty("unreviewed")
	named, err := ti.service.ListRequests(ctx, explicit)
	require.NoError(t, err)
	require.Len(t, named.Requests, 1)
}

func TestEnsureServerReview_RealAskUpgradesTheDossier(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	dossier, err := ti.service.EnsureServerReview(ctx, ensurePayload("https://upgrade.example.com/mcp"))
	require.NoError(t, err)

	asked, err := ti.service.CreateRequest(ctx, createPayload("server_url", "https://upgrade.example.com/mcp", "need it for release notes"))
	require.NoError(t, err)
	require.Equal(t, dossier.ID, asked.ID)
	require.Equal(t, "requested", asked.Status)
	require.Equal(t, 1, asked.RequesterCount)

	queued, err := ti.service.ListRequests(ctx, listPayload())
	require.NoError(t, err)
	require.Len(t, queued.Requests, 1)
}

func TestEnsureServerReview_NeverDowngradesADecidedRow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	asked, err := ti.service.CreateRequest(ctx, createPayload("server_url", "https://decided.example.com/mcp", "need it"))
	require.NoError(t, err)

	ensured, err := ti.service.EnsureServerReview(ctx, ensurePayload("https://decided.example.com/mcp"))
	require.NoError(t, err)
	require.Equal(t, asked.ID, ensured.ID)
	require.Equal(t, "requested", ensured.Status)
}

func TestEnsureServerReview_RejectsNonURLTargets(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.EnsureServerReview(ctx, ensurePayload("npx -y some-package"))
	requireOopsCode(t, err, oops.CodeBadRequest)
}
