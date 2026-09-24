package admin

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestAdminActor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		auth            *contextvalues.AdminAuthContext
		wantActor       urn.Principal
		wantDisplayName *string
		wantEmail       *string
	}{
		{
			name: "authenticated admin",
			auth: &contextvalues.AdminAuthContext{
				OIDCSubject: "oidc-subject",
				Name:        "Test Operator",
				Email:       "operator@example.test",
			},
			wantActor:       urn.NewPrincipal(urn.PrincipalTypeUser, "oidc-subject"),
			wantDisplayName: new("Test Operator"),
			wantEmail:       new("operator@example.test"),
		},
		{
			name: "email display name fallback",
			auth: &contextvalues.AdminAuthContext{
				OIDCSubject: "oidc-subject",
				Email:       "operator@example.test",
			},
			wantActor:       urn.NewPrincipal(urn.PrincipalTypeUser, "oidc-subject"),
			wantDisplayName: new("operator@example.test"),
			wantEmail:       new("operator@example.test"),
		},
		{
			name:      "missing admin context",
			wantActor: urn.NewPrincipal(urn.PrincipalTypeUser, "system"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			if tt.auth != nil {
				ctx = contextvalues.SetAdminAuthContext(ctx, tt.auth)
			}

			actor, displayName, operatorEmail := adminActor(ctx)

			require.Equal(t, tt.wantActor, actor)
			require.Equal(t, tt.wantDisplayName, displayName)
			require.Equal(t, tt.wantEmail, operatorEmail)
		})
	}
}
