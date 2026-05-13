package hooks

import (
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestShadowMCPApprovalRequestURLUsesFragmentToken(t *testing.T) {
	t.Parallel()

	siteURL, err := url.Parse("https://app.example.test")
	require.NoError(t, err)
	service := &Service{
		logger:    testenv.NewLogger(t),
		siteURL:   siteURL,
		jwtSecret: "test-jwt-secret",
	}

	requestURL, ok := service.shadowMCPApprovalRequestURL(t.Context(), shadowMCPRequestLinkParams{
		OrganizationID:  "org_test",
		ProjectID:       "00000000-0000-0000-0000-000000000001",
		RequesterUserID: "user_test",
		AuditReason:     "blocked",
		Evidence: shadowmcp.AccessEvidence{
			FullURL:        "https://MCP.Example.com:443/sse#ignored",
			URLHost:        "",
			ServerIdentity: "Example MCP",
		},
		ToolName:     "list_issues",
		ToolInput:    map[string]any{"prompt": strings.Repeat("x", 10_000)},
		RiskPolicyID: "00000000-0000-0000-0000-000000000002",
	})
	require.True(t, ok)

	parsed, err := url.Parse(requestURL)
	require.NoError(t, err)
	require.Empty(t, parsed.RawQuery)
	require.Equal(t, "https", parsed.Scheme)
	require.Equal(t, "app.example.test", parsed.Host)
	require.Equal(t, "/shadow-mcp/request", parsed.Path)

	fragment, err := url.ParseQuery(parsed.Fragment)
	require.NoError(t, err)
	require.NotContains(t, requestURL, "?request_token=")
	require.Contains(t, fragment.Get("request_token"), "smar1.")
	require.Less(t, len(requestURL), 1200, "approval link should not embed full tool input")
}

func TestShadowMCPApprovalRequestURLRequiresEvidence(t *testing.T) {
	t.Parallel()

	siteURL, err := url.Parse("https://app.example.test")
	require.NoError(t, err)
	service := &Service{
		logger:    testenv.NewLogger(t),
		siteURL:   siteURL,
		jwtSecret: "test-jwt-secret",
	}

	_, ok := service.shadowMCPApprovalRequestURL(t.Context(), shadowMCPRequestLinkParams{
		OrganizationID:  "org_test",
		ProjectID:       "00000000-0000-0000-0000-000000000001",
		RequesterUserID: "user_test",
		AuditReason:     "blocked",
		Evidence:        shadowmcp.AccessEvidence{},
		ToolName:        "list_issues",
	})
	require.False(t, ok)
}
