package audit

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func sessionContext(t *testing.T, sessionID string) context.Context {
	t.Helper()

	return contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org_test",
		UserID:               "user_test",
		SessionID:            &sessionID,
	})
}

func TestActingIdentityFromContext_Derivation(t *testing.T) {
	t.Parallel()

	sessionID := "session_test"

	tests := []struct {
		name    string
		ctx     func(t *testing.T) context.Context
		surface Surface
	}{
		{
			name:    "no signal is unknown rather than assumed",
			ctx:     func(t *testing.T) context.Context { t.Helper(); return t.Context() },
			surface: SurfaceUnknown,
		},
		{
			name:    "a session is the dashboard",
			ctx:     func(t *testing.T) context.Context { t.Helper(); return sessionContext(t, sessionID) },
			surface: SurfaceDashboard,
		},
		{
			name: "an admin session is the admin app",
			ctx: func(t *testing.T) context.Context {
				t.Helper()
				return contextvalues.SetAdminAuthContext(t.Context(), &contextvalues.AdminAuthContext{
					SessionID:   "admin_session_test",
					OIDCSubject: "admin_subject_test",
					Name:        "Test Operator",
					Email:       "operator@example.com",
				})
			},
			surface: SurfaceAdmin,
		},
		{
			name: "an API key is not the dashboard",
			ctx: func(t *testing.T) context.Context {
				t.Helper()
				return contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
					ActiveOrganizationID: "org_test",
					APIKeyID:             "key_test",
				})
			},
			surface: SurfaceAPIKey,
		},
		{
			name: "an auth context with neither session nor key is unknown",
			ctx: func(t *testing.T) context.Context {
				t.Helper()
				return contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
					ActiveOrganizationID: "org_test",
				})
			},
			surface: SurfaceUnknown,
		},
		{
			name: "an empty session id does not count as a dashboard session",
			ctx: func(t *testing.T) context.Context {
				t.Helper()
				empty := ""
				return contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
					ActiveOrganizationID: "org_test",
					SessionID:            &empty,
				})
			},
			surface: SurfaceUnknown,
		},
		{
			name: "an assistant acting on a session is not the dashboard",
			ctx: func(t *testing.T) context.Context {
				t.Helper()
				return contextvalues.SetAssistantPrincipal(sessionContext(t, sessionID), contextvalues.AssistantPrincipal{
					AssistantID: uuid.New(),
					ThreadID:    uuid.New(),
				})
			},
			surface: SurfaceProjectAssistant,
		},
		{
			name: "an explicit mark wins over a derived surface",
			ctx: func(t *testing.T) context.Context {
				t.Helper()
				return contextvalues.SetActingSurface(sessionContext(t, sessionID), string(SurfacePlatformMCP))
			},
			surface: SurfacePlatformMCP,
		},
		{
			name: "platform break glass is a distinct closed surface",
			ctx: func(t *testing.T) context.Context {
				t.Helper()
				return contextvalues.SetActingSurface(sessionContext(t, sessionID), string(SurfacePlatformBreakGlass))
			},
			surface: SurfacePlatformBreakGlass,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.surface, actingIdentityFromContext(tt.ctx(t)).Surface)
		})
	}
}

// TestActingIdentityFromContext_UnrecognizedMarkIsUnknown is the cardinality
// guard. A package marking a surface passes a plain string, so the only thing
// keeping the column to a closed set is this rejection. Without it, any caller
// reaching SetActingSurface could mint a new value in the audit trail.
func TestActingIdentityFromContext_UnrecognizedMarkIsUnknown(t *testing.T) {
	t.Parallel()

	for _, mark := range []string{"totally-made-up", "DASHBOARD", "dashboard ", "'; DROP TABLE audit_logs; --"} {
		t.Run(mark, func(t *testing.T) {
			t.Parallel()

			ctx := contextvalues.SetActingSurface(t.Context(), mark)
			require.Equal(t, SurfaceUnknown, actingIdentityFromContext(ctx).Surface)
		})
	}
}

// TestActingIdentityFromContext_ClientIDOnlyFromOAuthClient is the leak guard.
// The client identity must come from the token's registered client record, so
// a request that presents no OAuth client records nothing at all — never a user
// id, session id or API key id standing in for one.
func TestActingIdentityFromContext_ClientIDOnlyFromOAuthClient(t *testing.T) {
	t.Parallel()

	t.Run("absent without an OAuth client", func(t *testing.T) {
		t.Parallel()

		ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
			ActiveOrganizationID: "org_test",
			UserID:               "user_secret",
			APIKeyID:             "key_secret",
		})

		require.Empty(t, actingIdentityFromContext(ctx).ClientID)
	})

	t.Run("present when the token named one", func(t *testing.T) {
		t.Parallel()

		ctx := contextvalues.SetOAuthClientID(sessionContext(t, "session_test"), "client_registered")
		identity := actingIdentityFromContext(ctx)

		require.Equal(t, "client_registered", identity.ClientID)
		require.Equal(t, SurfaceDashboard, identity.Surface)
	})
}

// TestKnownSurfaces_AreLowCardinality pins the size of the set. The column is
// faceted in the audit feed, so growth here is a product decision rather than
// something a new surface should pick up silently.
func TestKnownSurfaces_AreLowCardinality(t *testing.T) {
	t.Parallel()

	require.Len(t, knownSurfaces, 7)
	require.Contains(t, knownSurfaces, SurfaceUnknown)
	require.Contains(t, knownSurfaces, SurfaceAdmin)
	require.Contains(t, knownSurfaces, SurfacePlatformBreakGlass)
}
