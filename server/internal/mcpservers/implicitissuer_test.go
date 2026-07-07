package mcpservers_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
)

func TestEligibleForImplicitIssuer(t *testing.T) {
	t.Parallel()

	set := uuid.NullUUID{UUID: uuid.New(), Valid: true}
	unset := uuid.NullUUID{UUID: uuid.Nil, Valid: false}

	cases := []struct {
		name       string
		visibility string
		issuer     uuid.NullUUID
		remote     uuid.NullUUID
		tunneled   uuid.NullUUID
		toolset    uuid.NullUUID
		want       bool
	}{
		{"private remote no issuer", mcpservers.VisibilityPrivate, unset, set, unset, unset, true},
		{"private tunneled no issuer", mcpservers.VisibilityPrivate, unset, unset, set, unset, true},
		{"explicit issuer wins", mcpservers.VisibilityPrivate, set, set, unset, unset, false},
		{"public remote excluded", mcpservers.VisibilityPublic, unset, set, unset, unset, false},
		{"disabled excluded", mcpservers.VisibilityDisabled, unset, set, unset, unset, false},
		{"toolset backend excluded", mcpservers.VisibilityPrivate, unset, unset, unset, set, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := &repo.McpServer{
				UserSessionIssuerID: tc.issuer,
				Visibility:          tc.visibility,
				RemoteMcpServerID:   tc.remote,
				TunneledMcpServerID: tc.tunneled,
				ToolsetID:           tc.toolset,
			}
			require.Equal(t, tc.want, mcpservers.EligibleForImplicitIssuer(server))
		})
	}
}
