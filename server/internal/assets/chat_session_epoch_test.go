package assets_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	assetsvc "github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestChatSessionEpoch_AssetEndpoints(t *testing.T) {
	t.Parallel()

	for _, operation := range []string{"download", "sign URL"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestAssetsService(t)
			ac, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)

			content := "attachment contents"
			writer, uri, err := ti.storage.Write(ctx, "attachment.txt", "text/plain", int64(len(content)))
			require.NoError(t, err)

			_, err = io.WriteString(writer, content)
			require.NoError(t, err)
			require.NoError(t, writer.Close())

			asset, err := ti.repo.CreateAsset(ctx, repo.CreateAssetParams{
				Name: "attachment.txt", Url: uri.String(), ProjectID: *ac.ProjectID,
				OrganizationID: ac.ActiveOrganizationID, Sha256: "test-hash", Kind: "chat_attachment",
				ContentType: "text/plain", ContentLength: int64(len(content)),
			})
			require.NoError(t, err)

			userID := uuid.NewString()
			cutoff := time.Now().Add(-time.Minute)
			session := sessions.Session{UserID: userID, SessionID: uuid.NewString(), ActiveOrganizationID: ac.ActiveOrganizationID}
			require.NoError(t, ti.sessionManager.StoreSessionIssuedAt(ctx, session, cutoff.Add(-time.Second)))

			claims := chatsessions.ChatSessionClaims{
				OrgID: ac.ActiveOrganizationID, ProjectID: ac.ProjectID.String(),
				OrganizationSlug: ac.OrganizationSlug, ProjectSlug: *ac.ProjectSlug,
				UserID: userID, SessionID: &session.SessionID,
			}
			token, _, err := ti.chatSessionsManager.GenerateToken(ctx, claims, "https://example.com", 3600)
			require.NoError(t, err)

			mux := goahttp.NewMuxer()
			assetsvc.Attach(mux, ti.service)
			request := func(token string) *httptest.ResponseRecorder {
				var req *http.Request
				if operation == "download" {
					req = httptest.NewRequest(http.MethodGet, "/rpc/assets.serveChatAttachment?id="+asset.ID.String()+"&project_id="+ac.ProjectID.String(), nil)
				} else {
					body := fmt.Sprintf(`{"id":%q,"project_id":%q}`, asset.ID.String(), ac.ProjectID.String())
					req = httptest.NewRequest(http.MethodPost, "/rpc/assets.createSignedChatAttachmentURL", strings.NewReader(body))
					req.Header.Set("Content-Type", "application/json")
				}
				req.Header.Set(constants.ChatSessionsTokenHeader, token)
				req.Header.Set(constants.ProjectHeader, *ac.ProjectSlug)
				// Supply permissions without authenticating the request: the HTTP endpoint
				// must still validate the JWT and its parent session on every request.
				req = req.WithContext(authztest.WithAdminGrants(t.Context()))
				response := httptest.NewRecorder()
				mux.ServeHTTP(response, req)
				return response
			}
			before := request(token)
			require.Equal(t, http.StatusOK, before.Code, before.Body.String())

			if operation == "download" {
				require.Equal(t, content, before.Body.String())
			}
			require.NoError(t, ti.sessionManager.RaiseUserEpoch(ctx, userID, cutoff))

			after := request(token)
			require.Equal(t, http.StatusUnauthorized, after.Code, after.Body.String())

			session.SessionID = uuid.NewString()
			require.NoError(t, ti.sessionManager.StoreSessionIssuedAt(ctx, session, cutoff.Add(time.Second)))

			claims.SessionID = &session.SessionID
			fresh, _, err := ti.chatSessionsManager.GenerateToken(ctx, claims, "https://example.com", 3600)
			require.NoError(t, err)

			renewed := request(fresh)
			require.Equal(t, http.StatusOK, renewed.Code, renewed.Body.String())

			redisClient, err := infra.NewRedisClient(t, 0)
			require.NoError(t, err)
			require.NoError(t, redisClient.Set(ctx, "sessions:valid_after:"+userID, "corrupt-marker", time.Minute).Err())

			unavailable := request(fresh)
			require.Equal(t, http.StatusServiceUnavailable, unavailable.Code, unavailable.Body.String())
		})
	}
}
