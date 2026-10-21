package chatsessions

import (
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/chat_sessions"
	sessionauth "github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestCreateSensitiveContentAccess(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		orgID    string
		identity string
		denied   bool
	}{
		{name: "support impersonation", orgID: "org_test", identity: "support", denied: true},
		{name: "WorkOS impersonation", orgID: "org_test", identity: "workos", denied: true},
		{name: "normal session", orgID: "org_test", identity: "normal"},
		{name: "API key", orgID: "org_test", identity: "api-key"},
		{name: "demo support", orgID: constants.DemoOrganizationID, identity: "support"},
		{name: "demo WorkOS", orgID: constants.DemoOrganizationID, identity: "workos"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			projectID := uuid.New()
			projectSlug := "test-project"
			sessionID := uuid.NewString()
			authCtx := &contextvalues.AuthContext{
				ActiveOrganizationID: tc.orgID,
				OrganizationSlug:     "test-org",
				ProjectID:            &projectID,
				ProjectSlug:          &projectSlug,
				UserID:               "test-user",
				SessionID:            &sessionID,
			}
			ctx := t.Context()
			switch tc.identity {
			case "support":
				authCtx.IsAdmin = true
				authCtx.SupportOrganizationID = tc.orgID
				ctx = contextvalues.WithValidatedSupportSession(ctx, authCtx)
			case "workos", "normal":
				ctx = contextvalues.WithValidatedGramSession(ctx, authCtx, tc.identity == "workos")
			case "api-key":
				authCtx.UserID = ""
				authCtx.SessionID = nil
				authCtx.APIKeyID = "test-key"
				ctx = contextvalues.WithLegacyAPIKeyAuthorization(ctx, authCtx)
			}

			const secret = "test-chat-session-secret"
			logger := testenv.NewLogger(t)
			// Create signs tokens without database or Redis I/O. Exercise the public
			// endpoint with a real token manager and otherwise valid auth context.
			svc := &Service{
				tracer:              testenv.NewTracerProvider(t).Tracer("test"),
				logger:              logger,
				chatSessionsManager: sessionauth.NewManager(logger, nil, secret),
			}
			result, err := svc.Create(ctx, &gen.CreatePayload{
				EmbedOrigin:  "https://example.com",
				ExpiresAfter: 3600,
			})
			if tc.denied {
				require.Error(t, err)
				require.Nil(t, result, "impersonation must not mint a token that loses its provenance")
				var denied *oops.ShareableError
				require.ErrorAs(t, err, &denied)
				require.Equal(t, oops.CodeForbidden, denied.Code)
				return
			}
			require.NoError(t, err)
			require.NotEmpty(t, result.ClientToken)
			claims := &sessionauth.ChatSessionClaims{}
			token, err := jwt.ParseWithClaims(result.ClientToken, claims, func(*jwt.Token) (any, error) {
				return []byte(secret), nil
			})
			require.NoError(t, err)
			require.True(t, token.Valid)
			require.Equal(t, tc.orgID, claims.OrgID)
			require.Equal(t, projectID.String(), claims.ProjectID)
			require.Equal(t, authCtx.UserID, claims.UserID)
			require.Equal(t, authCtx.SessionID, claims.SessionID)
			require.Equal(t, authCtx.APIKeyID, claims.APIKeyID)
		})
	}
}
