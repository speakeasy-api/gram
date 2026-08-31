package mcpservers

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestVerifyUpstreamAuthorization(t *testing.T) {
	t.Parallel()

	set := uuid.NullUUID{UUID: uuid.New(), Valid: true}
	unset := uuid.NullUUID{UUID: uuid.Nil, Valid: false}

	tests := []struct {
		name                string
		visibility          string
		toolsetID           uuid.NullUUID
		userSessionIssuerID uuid.NullUUID
		wantErr             string
	}{
		{
			name:                "upstream on a hosted backend with no issuer is the only accepted shape",
			visibility:          VisibilityUpstream,
			toolsetID:           set,
			userSessionIssuerID: unset,
			wantErr:             "",
		},
		{
			name:                "upstream without a toolset backend is rejected",
			visibility:          VisibilityUpstream,
			toolsetID:           unset,
			userSessionIssuerID: unset,
			wantErr:             "only toolset-backed MCP servers can serve direct upstream authorization",
		},
		{
			name:                "upstream gated by a user session issuer is rejected",
			visibility:          VisibilityUpstream,
			toolsetID:           set,
			userSessionIssuerID: set,
			wantErr:             "cannot also be gated by a user session issuer",
		},
		{
			// Both violations at once still reports the backend one; the
			// message only has to name a reason the write cannot proceed.
			name:                "upstream violating both rules is rejected",
			visibility:          VisibilityUpstream,
			toolsetID:           unset,
			userSessionIssuerID: set,
			wantErr:             "only toolset-backed MCP servers can serve direct upstream authorization",
		},
		{
			// The other visibilities own neither rule: a private or public
			// server is routinely both issuer-gated and proxied, so the check
			// must not leak onto them.
			name:                "private is unconstrained",
			visibility:          VisibilityPrivate,
			toolsetID:           unset,
			userSessionIssuerID: set,
			wantErr:             "",
		},
		{
			name:                "public is unconstrained",
			visibility:          VisibilityPublic,
			toolsetID:           unset,
			userSessionIssuerID: set,
			wantErr:             "",
		},
		{
			name:                "disabled is unconstrained",
			visibility:          VisibilityDisabled,
			toolsetID:           unset,
			userSessionIssuerID: set,
			wantErr:             "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := verifyUpstreamAuthorization(tt.visibility, tt.toolsetID, tt.userSessionIssuerID)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}
