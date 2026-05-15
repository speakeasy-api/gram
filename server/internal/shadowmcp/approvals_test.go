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

func TestCanonicalizeMatch(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want string
	}{
		// URL forms — host/scheme lowercased, trailing slash dropped.
		{"empty", "", ""},
		{"whitespace", "   ", ""},
		{"url canonical", "https://example.com/mcp", "https://example.com/mcp"},
		{"url upper host", "https://EXAMPLE.COM/mcp", "https://example.com/mcp"},
		{"url trailing slash", "https://example.com/mcp/", "https://example.com/mcp"},
		{"url bare host trailing slash", "https://example.com/", "https://example.com"},
		{"url upper scheme", "HTTPS://example.com/mcp", "https://example.com/mcp"},
		// Command / prefix forms — whitespace folded, surrounding spaces stripped.
		{"command canonical", "mise mcp", "mise mcp"},
		{"command surrounding spaces", "  mise mcp  ", "mise mcp"},
		{"command internal whitespace", "mise   mcp\t--flag", "mise mcp --flag"},
		{"command newlines", "/opt/bin/foo\n--flag", "/opt/bin/foo --flag"},
		{"server prefix", "mise", "mise"},
		// Non-URL string with a colon (eg "MCP:foo") must fall through to the
		// command branch, not be lossily reformatted by url.Parse.
		{"prefix-like with colon", "MCP:foo", "MCP:foo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, shadowmcp.CanonicalizeMatch(tc.in))
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
		Match:      "https://mcp.example.com/server",
		ServerName: "Example",
		ApprovedBy: "tester",
		ApprovedAt: time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC),
	}
	require.NoError(t, shadowmcp.AddShadowMCPApproval(t.Context(), c, projectID, policyID, first))

	second := shadowmcp.ShadowMCPApproval{
		Match:      "mise mcp",
		ServerName: "mise",
		ApprovedBy: "tester",
	}
	require.NoError(t, shadowmcp.AddShadowMCPApproval(t.Context(), c, projectID, policyID, second))

	got, err := shadowmcp.ListShadowMCPApprovals(t.Context(), c, projectID, policyID)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, first.Match, got[0].Match)
	assert.Equal(t, "Example", got[0].ServerName)
	assert.Equal(t, second.Match, got[1].Match)
	assert.False(t, got[1].ApprovedAt.IsZero(), "AddShadowMCPApproval must default ApprovedAt when zero")
}

func TestAddShadowMCPApproval_IsIdempotent(t *testing.T) {
	t.Parallel()
	c := newCacheForApprovalsTest(t)
	projectID := uuid.NewString()
	policyID := uuid.NewString()

	for i := 0; i < 3; i++ {
		require.NoError(t, shadowmcp.AddShadowMCPApproval(t.Context(), c, projectID, policyID, shadowmcp.ShadowMCPApproval{
			Match:      "https://mcp.example.com/server",
			ServerName: "Example",
		}))
	}

	got, err := shadowmcp.ListShadowMCPApprovals(t.Context(), c, projectID, policyID)
	require.NoError(t, err)
	assert.Len(t, got, 1, "re-adding the same match must not duplicate")
}

func TestAddShadowMCPApproval_EmptyMatchRejected(t *testing.T) {
	t.Parallel()
	c := newCacheForApprovalsTest(t)

	err := shadowmcp.AddShadowMCPApproval(t.Context(), c, uuid.NewString(), uuid.NewString(), shadowmcp.ShadowMCPApproval{Match: "   "})
	require.Error(t, err)
}

// IsShadowMCPApproved accepts multiple candidate identifiers for the same
// call — the hook matcher supplies URL + Command + server-prefix at once so
// an approval recorded against any one of them allows the call.
func TestIsShadowMCPApproved_MatchesAnyCandidate(t *testing.T) {
	t.Parallel()
	c := newCacheForApprovalsTest(t)
	projectID := uuid.NewString()
	policyID := uuid.NewString()

	require.NoError(t, shadowmcp.AddShadowMCPApproval(t.Context(), c, projectID, policyID, shadowmcp.ShadowMCPApproval{
		Match: "mise",
	}))

	// Approval was keyed on the server prefix; a call that supplies a
	// (URL, Command, prefix) triple should still resolve to approved.
	ok, err := shadowmcp.IsShadowMCPApproved(t.Context(), c, projectID, policyID, "", "mise mcp", "mise")
	require.NoError(t, err)
	assert.True(t, ok)

	// None of the candidates match a different server.
	ok, err = shadowmcp.IsShadowMCPApproved(t.Context(), c, projectID, policyID, "https://other/", "other mcp", "other")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestIsShadowMCPApproved_URLCanonicalization(t *testing.T) {
	t.Parallel()
	c := newCacheForApprovalsTest(t)
	projectID := uuid.NewString()
	policyID := uuid.NewString()

	require.NoError(t, shadowmcp.AddShadowMCPApproval(t.Context(), c, projectID, policyID, shadowmcp.ShadowMCPApproval{
		Match: "https://mcp.example.com/server/",
	}))

	cases := []struct {
		name      string
		candidate string
		want      bool
	}{
		{"exact", "https://mcp.example.com/server/", true},
		{"no trailing slash matches", "https://mcp.example.com/server", true},
		{"upper host matches", "https://MCP.EXAMPLE.COM/server", true},
		{"different host", "https://other.example.com/server", false},
		{"different path", "https://mcp.example.com/elsewhere", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := shadowmcp.IsShadowMCPApproved(t.Context(), c, projectID, policyID, tc.candidate)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestIsShadowMCPApproved_CommandWhitespaceNormalization(t *testing.T) {
	t.Parallel()
	c := newCacheForApprovalsTest(t)
	projectID := uuid.NewString()
	policyID := uuid.NewString()

	require.NoError(t, shadowmcp.AddShadowMCPApproval(t.Context(), c, projectID, policyID, shadowmcp.ShadowMCPApproval{
		Match: "mise mcp",
	}))

	ok, err := shadowmcp.IsShadowMCPApproved(t.Context(), c, projectID, policyID, "  mise   mcp ")
	require.NoError(t, err)
	assert.True(t, ok, "whitespace-normalized command should match")

	ok, err = shadowmcp.IsShadowMCPApproved(t.Context(), c, projectID, policyID, "mise mcp --extra")
	require.NoError(t, err)
	assert.False(t, ok, "different command must not match")
}

func TestIsShadowMCPApproved_ScopedByPolicy(t *testing.T) {
	t.Parallel()
	c := newCacheForApprovalsTest(t)
	projectID := uuid.NewString()
	policyA := uuid.NewString()
	policyB := uuid.NewString()

	require.NoError(t, shadowmcp.AddShadowMCPApproval(t.Context(), c, projectID, policyA, shadowmcp.ShadowMCPApproval{
		Match: "https://mcp.example.com/server",
	}))

	gotA, err := shadowmcp.IsShadowMCPApproved(t.Context(), c, projectID, policyA, "https://mcp.example.com/server")
	require.NoError(t, err)
	assert.True(t, gotA, "approval must apply to its own policy")

	gotB, err := shadowmcp.IsShadowMCPApproved(t.Context(), c, projectID, policyB, "https://mcp.example.com/server")
	require.NoError(t, err)
	assert.False(t, gotB, "approval for policyA must not leak to policyB")
}

func TestIsShadowMCPApproved_ScopedByProject(t *testing.T) {
	t.Parallel()
	c := newCacheForApprovalsTest(t)
	projectA := uuid.NewString()
	projectB := uuid.NewString()
	policyID := uuid.NewString()

	require.NoError(t, shadowmcp.AddShadowMCPApproval(t.Context(), c, projectA, policyID, shadowmcp.ShadowMCPApproval{
		Match: "https://mcp.example.com/server",
	}))

	gotA, err := shadowmcp.IsShadowMCPApproved(t.Context(), c, projectA, policyID, "https://mcp.example.com/server")
	require.NoError(t, err)
	assert.True(t, gotA)

	gotB, err := shadowmcp.IsShadowMCPApproved(t.Context(), c, projectB, policyID, "https://mcp.example.com/server")
	require.NoError(t, err)
	assert.False(t, gotB, "approval for projectA must not leak to projectB")
}

func TestRemoveShadowMCPApproval(t *testing.T) {
	t.Parallel()
	c := newCacheForApprovalsTest(t)
	projectID := uuid.NewString()
	policyID := uuid.NewString()

	require.NoError(t, shadowmcp.AddShadowMCPApproval(t.Context(), c, projectID, policyID, shadowmcp.ShadowMCPApproval{
		Match: "https://mcp.example.com/keep",
	}))
	require.NoError(t, shadowmcp.AddShadowMCPApproval(t.Context(), c, projectID, policyID, shadowmcp.ShadowMCPApproval{
		Match: "https://mcp.example.com/drop",
	}))

	require.NoError(t, shadowmcp.RemoveShadowMCPApproval(t.Context(), c, projectID, policyID, "https://mcp.example.com/drop"))

	got, err := shadowmcp.ListShadowMCPApprovals(t.Context(), c, projectID, policyID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "https://mcp.example.com/keep", got[0].Match)
}

func TestRemoveShadowMCPApproval_MissingIsNoop(t *testing.T) {
	t.Parallel()
	c := newCacheForApprovalsTest(t)
	projectID := uuid.NewString()
	policyID := uuid.NewString()

	err := shadowmcp.RemoveShadowMCPApproval(t.Context(), c, projectID, policyID, "https://mcp.example.com/never-approved")
	require.NoError(t, err, "revoking a never-approved match must be a no-op")
}

func TestRemoveShadowMCPApproval_LastEntryClearsKey(t *testing.T) {
	t.Parallel()
	c := newCacheForApprovalsTest(t)
	projectID := uuid.NewString()
	policyID := uuid.NewString()

	require.NoError(t, shadowmcp.AddShadowMCPApproval(t.Context(), c, projectID, policyID, shadowmcp.ShadowMCPApproval{
		Match: "https://mcp.example.com/only",
	}))
	require.NoError(t, shadowmcp.RemoveShadowMCPApproval(t.Context(), c, projectID, policyID, "https://mcp.example.com/only"))

	got, err := shadowmcp.ListShadowMCPApprovals(t.Context(), c, projectID, policyID)
	require.NoError(t, err)
	assert.Empty(t, got, "removing the last approval should leave the list empty")
}

func TestRemoveShadowMCPApproval_Normalizes(t *testing.T) {
	t.Parallel()
	c := newCacheForApprovalsTest(t)
	projectID := uuid.NewString()
	policyID := uuid.NewString()

	require.NoError(t, shadowmcp.AddShadowMCPApproval(t.Context(), c, projectID, policyID, shadowmcp.ShadowMCPApproval{
		Match: "https://mcp.example.com/server/",
	}))
	require.NoError(t, shadowmcp.RemoveShadowMCPApproval(t.Context(), c, projectID, policyID, "https://MCP.EXAMPLE.COM/server"))

	got, err := shadowmcp.ListShadowMCPApprovals(t.Context(), c, projectID, policyID)
	require.NoError(t, err)
	assert.Empty(t, got, "remove must canonicalize the same way as add")
}
