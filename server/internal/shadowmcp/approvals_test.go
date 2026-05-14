package shadowmcp_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

func newCacheForApprovalsTest(t *testing.T) cache.Cache {
	t.Helper()
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	return cache.NewRedisCacheAdapter(redisClient)
}

func TestCanonicalizeApprovalURL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"whitespace", "   ", ""},
		{"already canonical", "https://example.com/mcp", "https://example.com/mcp"},
		{"upper host", "https://EXAMPLE.COM/mcp", "https://example.com/mcp"},
		{"trailing slash", "https://example.com/mcp/", "https://example.com/mcp"},
		{"bare host trailing slash", "https://example.com/", "https://example.com"},
		{"upper scheme", "HTTPS://example.com/mcp", "https://example.com/mcp"},
		{"malformed passes through", "not a url", "not a url"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, shadowmcp.CanonicalizeApprovalURL(tc.in))
		})
	}
}

func TestListShadowMCPApprovals_EmptyReturnsNil(t *testing.T) {
	t.Parallel()
	c := newCacheForApprovalsTest(t)

	got, err := shadowmcp.ListShadowMCPApprovals(t.Context(), c, uuid.NewString(), uuid.NewString())
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestAddAndListShadowMCPApprovals(t *testing.T) {
	t.Parallel()
	c := newCacheForApprovalsTest(t)
	projectID := uuid.NewString()
	policyID := uuid.NewString()

	first := shadowmcp.ShadowMCPApproval{
		URL:        "https://mcp.example.com/server",
		ServerName: "Example",
		ApprovedBy: "tester",
		ApprovedAt: time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC),
	}
	require.NoError(t, shadowmcp.AddShadowMCPApproval(t.Context(), c, projectID, policyID, first))

	second := shadowmcp.ShadowMCPApproval{
		URL:        "https://mcp.other.com/server",
		ServerName: "Other",
		ApprovedBy: "tester",
	}
	require.NoError(t, shadowmcp.AddShadowMCPApproval(t.Context(), c, projectID, policyID, second))

	got, err := shadowmcp.ListShadowMCPApprovals(t.Context(), c, projectID, policyID)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, first.URL, got[0].URL)
	assert.Equal(t, "Example", got[0].ServerName)
	assert.Equal(t, second.URL, got[1].URL)
	assert.False(t, got[1].ApprovedAt.IsZero(), "AddShadowMCPApproval must default ApprovedAt when zero")
}

func TestAddShadowMCPApproval_IsIdempotent(t *testing.T) {
	t.Parallel()
	c := newCacheForApprovalsTest(t)
	projectID := uuid.NewString()
	policyID := uuid.NewString()

	for i := 0; i < 3; i++ {
		require.NoError(t, shadowmcp.AddShadowMCPApproval(t.Context(), c, projectID, policyID, shadowmcp.ShadowMCPApproval{
			URL:        "https://mcp.example.com/server",
			ServerName: "Example",
		}))
	}

	got, err := shadowmcp.ListShadowMCPApprovals(t.Context(), c, projectID, policyID)
	require.NoError(t, err)
	assert.Len(t, got, 1, "re-adding the same URL must not duplicate")
}

func TestAddShadowMCPApproval_EmptyURLRejected(t *testing.T) {
	t.Parallel()
	c := newCacheForApprovalsTest(t)

	err := shadowmcp.AddShadowMCPApproval(t.Context(), c, uuid.NewString(), uuid.NewString(), shadowmcp.ShadowMCPApproval{URL: "   "})
	require.Error(t, err)
}

func TestIsShadowMCPURLApproved(t *testing.T) {
	t.Parallel()
	c := newCacheForApprovalsTest(t)
	projectID := uuid.NewString()
	policyID := uuid.NewString()

	require.NoError(t, shadowmcp.AddShadowMCPApproval(t.Context(), c, projectID, policyID, shadowmcp.ShadowMCPApproval{
		URL: "https://mcp.example.com/server/",
	}))

	cases := []struct {
		name string
		url  string
		want bool
	}{
		{"exact", "https://mcp.example.com/server/", true},
		{"no trailing slash matches", "https://mcp.example.com/server", true},
		{"upper host matches", "https://MCP.EXAMPLE.COM/server", true},
		{"different host", "https://other.example.com/server", false},
		{"different path", "https://mcp.example.com/elsewhere", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := shadowmcp.IsShadowMCPURLApproved(t.Context(), c, projectID, policyID, tc.url)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestIsShadowMCPURLApproved_ScopedByPolicy(t *testing.T) {
	t.Parallel()
	c := newCacheForApprovalsTest(t)
	projectID := uuid.NewString()
	policyA := uuid.NewString()
	policyB := uuid.NewString()

	require.NoError(t, shadowmcp.AddShadowMCPApproval(t.Context(), c, projectID, policyA, shadowmcp.ShadowMCPApproval{
		URL: "https://mcp.example.com/server",
	}))

	gotA, err := shadowmcp.IsShadowMCPURLApproved(t.Context(), c, projectID, policyA, "https://mcp.example.com/server")
	require.NoError(t, err)
	assert.True(t, gotA, "approval must apply to its own policy")

	gotB, err := shadowmcp.IsShadowMCPURLApproved(t.Context(), c, projectID, policyB, "https://mcp.example.com/server")
	require.NoError(t, err)
	assert.False(t, gotB, "approval for policyA must not leak to policyB")
}

func TestIsShadowMCPURLApproved_ScopedByProject(t *testing.T) {
	t.Parallel()
	c := newCacheForApprovalsTest(t)
	projectA := uuid.NewString()
	projectB := uuid.NewString()
	policyID := uuid.NewString()

	require.NoError(t, shadowmcp.AddShadowMCPApproval(t.Context(), c, projectA, policyID, shadowmcp.ShadowMCPApproval{
		URL: "https://mcp.example.com/server",
	}))

	gotA, err := shadowmcp.IsShadowMCPURLApproved(t.Context(), c, projectA, policyID, "https://mcp.example.com/server")
	require.NoError(t, err)
	assert.True(t, gotA)

	gotB, err := shadowmcp.IsShadowMCPURLApproved(t.Context(), c, projectB, policyID, "https://mcp.example.com/server")
	require.NoError(t, err)
	assert.False(t, gotB, "approval for projectA must not leak to projectB")
}

func TestRemoveShadowMCPApproval(t *testing.T) {
	t.Parallel()
	c := newCacheForApprovalsTest(t)
	projectID := uuid.NewString()
	policyID := uuid.NewString()

	require.NoError(t, shadowmcp.AddShadowMCPApproval(t.Context(), c, projectID, policyID, shadowmcp.ShadowMCPApproval{
		URL: "https://mcp.example.com/keep",
	}))
	require.NoError(t, shadowmcp.AddShadowMCPApproval(t.Context(), c, projectID, policyID, shadowmcp.ShadowMCPApproval{
		URL: "https://mcp.example.com/drop",
	}))

	require.NoError(t, shadowmcp.RemoveShadowMCPApproval(t.Context(), c, projectID, policyID, "https://mcp.example.com/drop"))

	got, err := shadowmcp.ListShadowMCPApprovals(t.Context(), c, projectID, policyID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "https://mcp.example.com/keep", got[0].URL)
}

func TestRemoveShadowMCPApproval_MissingIsNoop(t *testing.T) {
	t.Parallel()
	c := newCacheForApprovalsTest(t)
	projectID := uuid.NewString()
	policyID := uuid.NewString()

	err := shadowmcp.RemoveShadowMCPApproval(t.Context(), c, projectID, policyID, "https://mcp.example.com/never-approved")
	require.NoError(t, err, "revoking a never-approved URL must be a no-op")
}

func TestRemoveShadowMCPApproval_LastEntryClearsKey(t *testing.T) {
	t.Parallel()
	c := newCacheForApprovalsTest(t)
	projectID := uuid.NewString()
	policyID := uuid.NewString()

	require.NoError(t, shadowmcp.AddShadowMCPApproval(t.Context(), c, projectID, policyID, shadowmcp.ShadowMCPApproval{
		URL: "https://mcp.example.com/only",
	}))
	require.NoError(t, shadowmcp.RemoveShadowMCPApproval(t.Context(), c, projectID, policyID, "https://mcp.example.com/only"))

	got, err := shadowmcp.ListShadowMCPApprovals(t.Context(), c, projectID, policyID)
	require.NoError(t, err)
	assert.Empty(t, got, "removing the last approval should leave the list empty")
}

func TestRemoveShadowMCPApproval_NormalizesURL(t *testing.T) {
	t.Parallel()
	c := newCacheForApprovalsTest(t)
	projectID := uuid.NewString()
	policyID := uuid.NewString()

	require.NoError(t, shadowmcp.AddShadowMCPApproval(t.Context(), c, projectID, policyID, shadowmcp.ShadowMCPApproval{
		URL: "https://mcp.example.com/server/",
	}))
	require.NoError(t, shadowmcp.RemoveShadowMCPApproval(t.Context(), c, projectID, policyID, "https://MCP.EXAMPLE.COM/server"))

	got, err := shadowmcp.ListShadowMCPApprovals(t.Context(), c, projectID, policyID)
	require.NoError(t, err)
	assert.Empty(t, got, "remove must canonicalize the same way as add")
}
