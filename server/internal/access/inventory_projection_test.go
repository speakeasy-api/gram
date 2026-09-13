package access

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
)

// A project reader reaches the MCP half of the section too, so the same rule
// has to hold there: aggregate state survives, anything naming a person does
// not. LatestRequest is the easy one to miss — it is the only attribution on
// the row that hides inside a nested model.
func TestRedactShadowMCPInventoryAttributionDropsEveryNamedPerson(t *testing.T) {
	t.Parallel()

	server := &gen.ShadowMCPInventoryServer{
		CanonicalServerURL: "https://mcp.example.com/",
		ServerSlug:         "mcp-example-com",
		URLHost:            "mcp.example.com",
		TargetKind:         nil,
		ServerName:         nil,
		FirstSeen:          "2026-09-01T00:00:00Z",
		LastSeen:           "2026-09-10T00:00:00Z",
		LastCalled:         nil,
		ObservedUseCount:   12,
		UserCount:          new(3),
		TopUsers:           []string{"alex@example.com"},
		Access:             "unreviewed",
		AccessSummary:      nil,
		RequestCount:       1,
		LatestRequest: &gen.ShadowMCPInventoryRequestSummary{
			ID:              "00000000-0000-0000-0000-000000000001",
			PolicyID:        "00000000-0000-0000-0000-000000000002",
			RequesterUserID: "user_01",
			RequesterEmail:  "alex@example.com",
			RequestedAt:     "2026-09-09T00:00:00Z",
		},
		ApprovalRequest:  nil,
		AllowedPolicyIds: nil,
		BlockedPolicyIds: nil,
	}

	redactShadowMCPInventoryAttribution([]*gen.ShadowMCPInventoryServer{server})

	require.Nil(t, server.UserCount)
	require.Nil(t, server.TopUsers)
	require.Nil(t, server.LatestRequest, "the request summary names the requester by id and by email")

	require.Equal(t, 12, server.ObservedUseCount, "aggregate usage is what the tier is for")
	require.Equal(t, 1, server.RequestCount, "that requests exist is not attribution")
}
