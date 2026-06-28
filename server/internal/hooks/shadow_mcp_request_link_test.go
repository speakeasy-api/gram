package hooks

import (
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
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
		cache:     cache.NoopCache,
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
	require.Equal(t, "/risk-policy-bypass/request", parsed.Path)

	fragment, err := url.ParseQuery(parsed.Fragment)
	require.NoError(t, err)
	require.NotContains(t, requestURL, "?request_token=")
	require.Contains(t, fragment.Get("request_token"), "rpbr2.")
	require.Less(t, len(requestURL), 120, "approval link should be a short cache-backed id, not embedded state")
}

func TestShadowMCPApprovalRequestURLRequiresEvidence(t *testing.T) {
	t.Parallel()

	siteURL, err := url.Parse("https://app.example.test")
	require.NoError(t, err)
	service := &Service{
		logger:    testenv.NewLogger(t),
		siteURL:   siteURL,
		jwtSecret: "test-jwt-secret",
		cache:     cache.NoopCache,
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

func TestShadowMCPApprovalRequestURLAllowsServerIdentityEvidence(t *testing.T) {
	t.Parallel()

	siteURL, err := url.Parse("https://app.example.test")
	require.NoError(t, err)
	service := &Service{
		logger:    testenv.NewLogger(t),
		siteURL:   siteURL,
		jwtSecret: "test-jwt-secret",
		cache:     cache.NoopCache,
	}

	requestURL, ok := service.shadowMCPApprovalRequestURL(t.Context(), shadowMCPRequestLinkParams{
		OrganizationID:  "org_test",
		ProjectID:       "00000000-0000-0000-0000-000000000001",
		RequesterUserID: "user_test",
		AuditReason:     "blocked",
		Evidence: shadowmcp.AccessEvidence{
			FullURL:        "",
			URLHost:        "",
			ServerIdentity: "github",
		},
		ToolName:     "search",
		RiskPolicyID: "00000000-0000-0000-0000-000000000002",
	})
	require.True(t, ok)
	require.Contains(t, requestURL, "/risk-policy-bypass/request#request_token=rpbr2.")
}

func TestObservedShadowMCPName_HumanizesServerIdentity(t *testing.T) {
	t.Parallel()

	name := shadowmcp.ObservedName(shadowmcp.AccessEvidence{
		FullURL:        "",
		URLHost:        "",
		ServerIdentity: "claude_ai_calendly",
	}, "authenticate")

	require.NotNil(t, name)
	require.Equal(t, "Claude AI Calendly", *name)
}

func TestObservedShadowMCPName_PrefersURLHostOverServerIdentity(t *testing.T) {
	t.Parallel()

	name := shadowmcp.ObservedName(shadowmcp.AccessEvidence{
		FullURL:        "https://mcp.calendly.com/sse",
		URLHost:        "mcp.calendly.com",
		ServerIdentity: "claude_ai_calendly",
	}, "authenticate")

	require.NotNil(t, name)
	require.Equal(t, "mcp.calendly.com", *name)
}
