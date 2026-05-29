package access

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestShadowMCPApprovalRequestTokenEncryptsEvidence(t *testing.T) {
	t.Parallel()

	fullURL := "https://sensitive.example.com/mcp"
	toolCall := `{"secret":"customer-data"}`
	token, _, err := GenerateShadowMCPApprovalRequestToken("test-jwt-secret", ShadowMCPApprovalRequestTokenInput{
		OrganizationID:  "org_token",
		ProjectID:       "018f4d1d-0000-7000-8000-000000000001",
		RequesterUserID: "user_token",
		ObservedFullURL: &fullURL,
		ToolCall:        &toolCall,
	}, 5*time.Minute)
	require.NoError(t, err)

	require.True(t, strings.HasPrefix(token, shadowMCPApprovalRequestTokenPrefix))
	require.NotContains(t, token, "sensitive.example.com")
	require.NotContains(t, token, "customer-data")

	claims, err := parseShadowMCPApprovalRequestToken("test-jwt-secret", token)
	require.NoError(t, err)
	require.Equal(t, fullURL, *claims.ObservedFullURL)
	require.Equal(t, toolCall, *claims.ToolCall)
}
