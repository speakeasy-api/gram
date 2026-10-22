package assets_test

import (
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/gen/assets"
	assetsvc "github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

const sandboxedAttachmentCSP = "default-src 'none'; style-src 'unsafe-inline'; sandbox"

func TestChatAttachmentServeRoutes_ConfineAttachments(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	require.NotNil(t, authCtx.ProjectSlug)

	chatToken, _, err := ti.chatSessionsManager.GenerateToken(t.Context(), chatsessions.ChatSessionClaims{
		OrgID:            authCtx.ActiveOrganizationID,
		ProjectID:        authCtx.ProjectID.String(),
		OrganizationSlug: authCtx.OrganizationSlug,
		ProjectSlug:      *authCtx.ProjectSlug,
	}, "http://localhost", 3600)
	require.NoError(t, err)

	mux := goahttp.NewMuxer()
	assetsvc.Attach(mux, ti.service)

	tests := []struct {
		name        string
		contentType string
		body        []byte
		disposition string
	}{
		{name: "html", contentType: "text/html", body: []byte(`<script>fetch("/rpc/keys.list")</script>`), disposition: "attachment"},
		{name: "svg", contentType: "image/svg+xml", body: []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), disposition: "attachment"},
		{name: "xml", contentType: "text/xml", body: []byte(`<html xmlns="http://www.w3.org/1999/xhtml"><script>alert(1)</script></html>`), disposition: "attachment"},
		{name: "pdf", contentType: "application/pdf", body: []byte("%PDF-1.7 fake"), disposition: "attachment"},
		{name: "png", contentType: "image/png", body: testPNG(t), disposition: "inline"},
		{name: "mp3", contentType: "audio/mpeg", body: []byte("ID3 fake mp3 bytes"), disposition: "inline"},
		{name: "plain text", contentType: "text/plain", body: []byte("hello from a text attachment"), disposition: "inline"},
		{name: "json", contentType: "application/json", body: []byte(`{"hello":"world"}`), disposition: "inline"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			uploaded, err := ti.service.UploadChatAttachment(ctx, &assets.UploadChatAttachmentForm{
				ApikeyToken:       nil,
				SessionToken:      nil,
				ProjectSlugInput:  nil,
				ChatSessionsToken: nil,
				ContentType:       tc.contentType,
				ContentLength:     int64(len(tc.body)),
			}, io.NopCloser(strings.NewReader(string(tc.body))))
			require.NoError(t, err)

			signed, err := ti.service.CreateSignedChatAttachmentURL(ctx, &assets.CreateSignedChatAttachmentURLForm{
				ApikeyToken:       nil,
				SessionToken:      nil,
				ProjectSlugInput:  nil,
				ChatSessionsToken: nil,
				ProjectID:         authCtx.ProjectID.String(),
				ID:                uploaded.Asset.ID,
				TTLSeconds:        nil,
			})
			require.NoError(t, err)

			authenticated := httptest.NewRequest(http.MethodGet, uploaded.URL, nil)
			authenticated.Header.Set(constants.ChatSessionsTokenHeader, chatToken)

			routes := map[string]*http.Request{
				"authenticated": authenticated,
				"signed":        httptest.NewRequest(http.MethodGet, signed.URL, nil),
			}
			for route, request := range routes {
				response := httptest.NewRecorder()
				mux.ServeHTTP(response, request)

				require.Equal(t, http.StatusOK, response.Code, "%s route", route)
				require.Equal(t, tc.contentType, response.Header().Get("Content-Type"), "%s route keeps the stored type", route)
				require.Equal(t, sandboxedAttachmentCSP, response.Header().Get("Content-Security-Policy"), "%s route", route)
				require.Equal(t, "nosniff", response.Header().Get("X-Content-Type-Options"), "%s route", route)

				disposition, params, err := mime.ParseMediaType(response.Header().Get("Content-Disposition"))
				require.NoError(t, err, "%s route", route)
				require.Equal(t, tc.disposition, disposition, "%s route", route)
				require.True(t, strings.HasPrefix(params["filename"], "attachment-"+uploaded.Asset.Sha256), "%s route filename %q", route, params["filename"])

				require.Equal(t, tc.body, response.Body.Bytes(), "%s route", route)
			}
		})
	}
}
