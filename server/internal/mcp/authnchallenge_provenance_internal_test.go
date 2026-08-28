package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// TestIdentityForSessionSubject pins the provenance mapping the issuer gate
// stamps after credential validation: only a concrete user subject yields
// authoritative acting-user provenance, and a user subject with no ID yields
// none at all.
func TestIdentityForSessionSubject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		subject urn.SessionSubject
		want    mcpidentity.Identity
		stamped bool
	}{
		{
			name:    "concrete user is authoritative",
			subject: urn.NewUserSubject("user_01J8EXAMPLE"),
			want:    mcpidentity.AuthenticatedUser("user_01J8EXAMPLE"),
			stamped: true,
		},
		{
			name:    "user subject without an ID stamps nothing",
			subject: urn.SessionSubject{Kind: urn.SessionSubjectKindUser, ID: ""},
			want:    mcpidentity.Identity{Kind: "", UserID: ""},
			stamped: false,
		},
		{
			name:    "api key never carries an acting user",
			subject: urn.SessionSubject{Kind: urn.SessionSubjectKindAPIKey, ID: "key_01J8EXAMPLE"},
			want:    mcpidentity.Identity{Kind: mcpidentity.KindAPIKey, UserID: ""},
			stamped: true,
		},
		{
			name:    "anonymous never carries an acting user",
			subject: urn.SessionSubject{Kind: urn.SessionSubjectKindAnonymous, ID: ""},
			want:    mcpidentity.Identity{Kind: mcpidentity.KindAnonymous, UserID: ""},
			stamped: true,
		},
		{
			name:    "unknown subject kind stamps nothing",
			subject: urn.SessionSubject{Kind: "device", ID: "dev_01J8EXAMPLE"},
			want:    mcpidentity.Identity{Kind: "", UserID: ""},
			stamped: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, stamped := identityForSessionSubject(tt.subject)
			require.Equal(t, tt.stamped, stamped)
			require.Equal(t, tt.want, got)
		})
	}
}
