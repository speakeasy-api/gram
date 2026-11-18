package admin

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"io"
	"maps"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"
)

func TestMCPServerSourcesRoutesRequireStaff(t *testing.T) {
	t.Parallel()
	for _, staff := range []bool{true, false} {
		email := "operator@example.com"
		if !staff {
			email = "person@external.test"
		}
		userinfo := userinfoOK("sub-admin", email)
		if !staff {
			userinfo = func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"sub":"sub-admin","email":"person@external.test","email_verified":true,"hd":"external.test"}`))
			}
		}
		svc := newTestSessionService(t, newTestOIDCClient(t, userinfo))
		svc.tracer = testenv.NewTracerProvider(t).Tracer("admin_test")
		mux := goahttp.NewMuxer()
		Attach(mux, svc)
		for _, route := range []string{"/admin/mcpServerSources", "/admin/mcpServerSourceDownload"} {
			for _, cookie := range []string{"", constants.SessionCookie, constants.AdminSessionCookie} {
				if staff && cookie == constants.AdminSessionCookie {
					continue
				}
				req := httptest.NewRequest(http.MethodGet, route, nil)
				if cookie != "" {
					value := "customer-session"
					if cookie == constants.AdminSessionCookie {
						value = makeAdminFeatureSession(t, t.Context(), svc, email)
					}
					req.AddCookie(&http.Cookie{Name: cookie, Value: value})
					req.Header.Set("Authorization", "Bearer customer-key")
				}
				rec := httptest.NewRecorder()
				SessionMiddleware(mux).ServeHTTP(rec, req)
				require.Equal(t, http.StatusUnauthorized, rec.Code, "%s cookie=%s staff=%v", route, cookie, staff)
			}
		}
	}
}

func TestMCPServerSourcesRoutesValidateTargets(t *testing.T) {
	t.Parallel()
	svc := newTestSessionService(t, newTestOIDCClient(t, userinfoOK("sub-admin", "operator@example.com")))
	sessionID := makeAdminFeatureSession(t, t.Context(), svc, "operator@example.com")
	mux := goahttp.NewMuxer()
	Attach(mux, svc)
	valid := url.Values{"organization_id": {"org_sources"}, "project_id": {uuid.NewString()}, "mcp_server_id": {uuid.NewString()}, "asset_id": {uuid.NewString()}}
	for _, route := range []string{"/admin/mcpServerSources", "/admin/mcpServerSourceDownload"} {
		for _, field := range []string{"organization_id", "project_id", "mcp_server_id", "asset_id"} {
			if route == "/admin/mcpServerSources" && field == "asset_id" {
				continue
			}
			for _, value := range []string{"", "invalid", uuid.Nil.String()} {
				if field == "organization_id" && value != "" {
					continue
				}
				query := url.Values{}
				maps.Copy(query, valid)
				query.Set(field, value)
				req := httptest.NewRequest(http.MethodGet, route+"?"+query.Encode(), nil)
				req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
				rec := httptest.NewRecorder()
				SessionMiddleware(mux).ServeHTTP(rec, req)
				require.Equal(t, http.StatusBadRequest, rec.Code, "%s %s=%s", route, field, value)
			}
		}
	}
}

type sourceBlobReader struct {
	body   io.ReadCloser
	err    error
	opened *url.URL
}

func (b *sourceBlobReader) Read(_ context.Context, u *url.URL) (io.ReadCloser, error) {
	b.opened = u
	return b.body, b.err
}

type sourceReadCloser struct {
	io.Reader
	closed bool
}

func (b *sourceReadCloser) Close() error { b.closed = true; return nil }

type sourceBrokenReader struct{}

func (sourceBrokenReader) Read([]byte) (int, error) { return 0, errors.New("broken stream") }

func TestMCPServerSourceDownloadStreamsAttachment(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"openapi.yaml", "../../spec.json", "..\\unsafe\r\n.html", "", "..", "スペック.yaml"} {
		body := &sourceReadCloser{Reader: strings.NewReader("openapi: 3.1.0\n")}
		store := &sourceBlobReader{body: body}
		svc := &Service{logger: testenv.NewLogger(t)}
		svc.SetMCPSourceStorage(store)
		source := adminOpenAPISource{AssetID: uuid.New(), Name: name, URL: "gs://test-bucket/source", ContentType: "text/html"}
		rec := httptest.NewRecorder()
		require.NoError(t, svc.downloadMCPServerSource(t.Context(), rec, source))
		require.Equal(t, source.URL, store.opened.String())
		require.True(t, body.closed)
		require.Equal(t, "openapi: 3.1.0\n", rec.Body.String())
		require.Equal(t, "application/octet-stream", rec.Header().Get("Content-Type"))
		require.Equal(t, "private, no-store", rec.Header().Get("Cache-Control"))
		require.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
		require.Equal(t, "sandbox", rec.Header().Get("Content-Security-Policy"))
		mediaType, params, err := mime.ParseMediaType(rec.Header().Get("Content-Disposition"))
		require.NoError(t, err)
		require.Equal(t, "attachment", mediaType)
		require.NotEmpty(t, params["filename"])
		require.NotContains(t, params["filename"], "/")
		require.NotContains(t, params["filename"], "\\")
		require.NotContains(t, params["filename"], "\r")
		require.NotContains(t, params["filename"], "\n")
	}
}

func TestMCPServerSourceDownloadStorageFailures(t *testing.T) {
	t.Parallel()
	source := adminOpenAPISource{AssetID: uuid.New(), Name: "spec.json", URL: "gs://test-bucket/source"}
	svc := &Service{logger: testenv.NewLogger(t)}
	rec := httptest.NewRecorder()
	require.Error(t, svc.downloadMCPServerSource(t.Context(), rec, source))
	store := &sourceBlobReader{err: errors.New("storage unavailable")}
	svc.SetMCPSourceStorage(store)
	require.Error(t, svc.downloadMCPServerSource(t.Context(), rec, source))
	require.Empty(t, rec.Header().Get("Content-Disposition"))
	body := &sourceReadCloser{Reader: sourceBrokenReader{}}
	store.body, store.err = body, nil
	require.PanicsWithValue(t, http.ErrAbortHandler, func() { _ = svc.downloadMCPServerSource(t.Context(), rec, source) })
	require.True(t, body.closed)
	require.Empty(t, rec.Body.String())
}

func TestMCPServerSourcesRoutesMembership(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	const orgID = "org_source_routes"
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: "Source Routes", slug: "source-routes"})
	projectID := seedProject(t, ctx, conn, orgID, "sources")
	toolsetID := seedToolset(t, ctx, conn, orgID, projectID, "sources", false)
	serverID := seedMCPServer(t, ctx, conn, projectID, toolsetID, "sources")
	deploymentID := seedCompletedDeployment(t, ctx, conn, orgID, projectID)
	includedID, excludedID := uuid.New(), uuid.New()
	for name, assetID := range map[string]uuid.UUID{"included": includedID, "excluded": excludedID} {
		documentID := uuid.New()
		_, err := conn.Exec(ctx, `INSERT INTO assets (id, project_id, name, url, kind, content_type, content_length, sha256) VALUES ($1,$2,$3,$4,'openapiv3','application/yaml',1,$3)`, assetID, projectID, name, "gs://source-routes/"+name)
		require.NoError(t, err)
		_, err = conn.Exec(ctx, `INSERT INTO deployments_openapiv3_assets (id, deployment_id, asset_id, name, slug) VALUES ($1,$2,$3,$4,$4)`, documentID, deploymentID, assetID, name)
		require.NoError(t, err)
		_, err = conn.Exec(ctx, `INSERT INTO http_tool_definitions (project_id, deployment_id, openapiv3_document_id, tool_urn, name, summary, description, server_env_var, http_method, path, schema_version, schema) VALUES ($1,$2,$3,$4,$5,'','','SERVER_URL','GET','/','1','{}')`, projectID, deploymentID, documentID, "tools:http:sources:"+name, name)
		require.NoError(t, err)
	}
	_, err := conn.Exec(ctx, `INSERT INTO toolset_versions (toolset_id, version, tool_urns) VALUES ($1,1,$2)`, toolsetID, []string{"tools:http:sources:included"})
	require.NoError(t, err)
	sessionID := makeAdminFeatureSession(t, ctx, svc, "operator@example.com")
	store := &sourceBlobReader{body: &sourceReadCloser{Reader: strings.NewReader("original source")}}
	svc.SetMCPSourceStorage(store)
	mux := goahttp.NewMuxer()
	Attach(mux, svc)
	request := func(route string, query url.Values) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, route+"?"+query.Encode(), nil)
		req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
		rec := httptest.NewRecorder()
		SessionMiddleware(mux).ServeHTTP(rec, req)
		return rec
	}
	query := url.Values{"organization_id": {orgID}, "project_id": {projectID.String()}, "mcp_server_id": {serverID.String()}}
	listed := request("/admin/mcpServerSources", query)
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	require.JSONEq(t, `{"sources":[{"asset_id":"`+includedID.String()+`","name":"included","filename":"included.yaml"}]}`, listed.Body.String())
	require.Nil(t, store.opened)
	query.Set("asset_id", excludedID.String())
	rejected := request("/admin/mcpServerSourceDownload", query)
	require.Equal(t, http.StatusNotFound, rejected.Code, rejected.Body.String())
	require.Nil(t, store.opened)
	query.Set("asset_id", includedID.String())
	downloaded := request("/admin/mcpServerSourceDownload", query)
	require.Equal(t, http.StatusOK, downloaded.Code, downloaded.Body.String())
	require.Equal(t, "original source", downloaded.Body.String())
	require.Equal(t, "gs://source-routes/included", store.opened.String())
	store.opened = nil
	query.Set("organization_id", "org_other")
	rejected = request("/admin/mcpServerSourceDownload", query)
	require.Equal(t, http.StatusNotFound, rejected.Code, rejected.Body.String())
	require.Nil(t, store.opened)
	query.Set("organization_id", orgID)
	_, err = conn.Exec(ctx, `INSERT INTO toolset_versions (toolset_id, version, tool_urns) VALUES ($1,2,'{}')`, toolsetID)
	require.NoError(t, err)
	listed = request("/admin/mcpServerSources", query)
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	require.JSONEq(t, `{"sources":[]}`, listed.Body.String())
	rejected = request("/admin/mcpServerSourceDownload", query)
	require.Equal(t, http.StatusNotFound, rejected.Code, rejected.Body.String())
	require.Nil(t, store.opened)
}
