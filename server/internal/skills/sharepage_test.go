package skills_test

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
)

// serveSharedSkillRequest invokes one of the raw share-page handlers with the
// chi route parameter and optional custom-domain context the middleware would
// normally provide.
func serveSharedSkillRequest(
	t *testing.T,
	ctx context.Context,
	handler func(w http.ResponseWriter, r *http.Request) error,
	token string,
	domainCtx *customdomains.Context,
) (*httptest.ResponseRecorder, error) {
	t.Helper()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("token", token)
	reqCtx := context.WithValue(ctx, chi.RouteCtxKey, rctx)
	if domainCtx != nil {
		reqCtx = customdomains.WithContext(reqCtx, domainCtx)
	}

	req := httptest.NewRequest(http.MethodGet, "/shared/skills/"+token, nil).WithContext(reqCtx)
	rr := httptest.NewRecorder()
	return rr, handler(rr, req)
}

func TestServeSharedSkillPageOnCustomDomain(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created, err := ti.service.Create(ctx, &gen.CreatePayload{
		Content:          skillManifest("domain-shared", "Served on a custom domain.", "# Getting started\n\nUse `gram` to *begin*."),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	link, err := ti.service.Share(ctx, &gen.SharePayload{SkillID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)

	rr, err := serveSharedSkillRequest(t, ctx, ti.service.ServeSharedSkillPage, link.Token, &customdomains.Context{
		OrganizationID: ti.authContext.ActiveOrganizationID,
		Domain:         "mcp.customer.example",
		DomainID:       uuid.New(),
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rr.Code)
	require.Contains(t, rr.Header().Get("Content-Type"), "text/html")
	require.Equal(t, "noindex, nofollow", rr.Header().Get("X-Robots-Tag"))
	require.Equal(t, "no-referrer", rr.Header().Get("Referrer-Policy"))
	require.Equal(t, "private, max-age=300", rr.Header().Get("Cache-Control"))
	require.NotEmpty(t, rr.Header().Get("Content-Security-Policy"))

	body := rr.Body.String()
	require.Contains(t, body, created.Skill.DisplayName)
	require.Contains(t, body, "Served on a custom domain.")
	// The Markdown body renders as HTML with the frontmatter stripped.
	require.Contains(t, body, "Getting started")
	require.Contains(t, body, "<code>gram</code>")
	require.NotContains(t, body, "name: domain-shared")
	require.Contains(t, body, "/shared/skills/"+link.Token+"/SKILL.md")
	require.Contains(t, body, "Powered by Speakeasy")
}

func TestServeSharedSkillPageWrongOrganizationDomain(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "cross-tenant-skill", "Must not leak across domains.")
	link, err := ti.service.Share(ctx, &gen.SharePayload{SkillID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)

	rr, err := serveSharedSkillRequest(t, ctx, ti.service.ServeSharedSkillPage, link.Token, &customdomains.Context{
		OrganizationID: "other-org-" + uuid.NewString(),
		Domain:         "mcp.other.example",
		DomainID:       uuid.New(),
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, rr.Code)
	require.Contains(t, rr.Body.String(), "This skill isn't available")
	require.NotContains(t, rr.Body.String(), created.Skill.DisplayName)
}

func TestServeSharedSkillPageRevokedToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "revoked-share", "Shared then revoked.")
	link, err := ti.service.Share(ctx, &gen.SharePayload{SkillID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.NoError(t, ti.service.Unshare(ctx, &gen.UnsharePayload{SkillID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}))

	rr, err := serveSharedSkillRequest(t, ctx, ti.service.ServeSharedSkillPage, link.Token, &customdomains.Context{
		OrganizationID: ti.authContext.ActiveOrganizationID,
		Domain:         "mcp.customer.example",
		DomainID:       uuid.New(),
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, rr.Code)
	require.Contains(t, rr.Body.String(), "This skill isn't available")
}

func TestServeSharedSkillPagePlatformHostRedirects(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "platform-share", "Redirects to the dashboard.")
	link, err := ti.service.Share(ctx, &gen.SharePayload{SkillID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)

	rr, err := serveSharedSkillRequest(t, ctx, ti.service.ServeSharedSkillPage, link.Token, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusFound, rr.Code)
	require.Equal(t, "https://app.getgram.test/shared/skills/"+link.Token, rr.Header().Get("Location"))
}

func TestServeSharedSkillPageSanitizesMarkdown(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created, err := ti.service.Create(ctx, &gen.CreatePayload{
		Content:          skillManifest("hostile-markdown", "Sanitized output.", "# Safe title\n\n<script>alert(1)</script>\n\n[link](javascript:alert(2))"),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	link, err := ti.service.Share(ctx, &gen.SharePayload{SkillID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)

	rr, err := serveSharedSkillRequest(t, ctx, ti.service.ServeSharedSkillPage, link.Token, &customdomains.Context{
		OrganizationID: ti.authContext.ActiveOrganizationID,
		Domain:         "mcp.customer.example",
		DomainID:       uuid.New(),
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rr.Code)

	body := rr.Body.String()
	require.Contains(t, body, "Safe title")
	require.NotContains(t, body, "<script>")
	require.NotContains(t, body, "javascript:alert")
}

func TestServeSharedSkillMarkdownOnCustomDomain(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	manifest := skillManifest("raw-download", "Raw SKILL.md download.", "# Raw body")
	created, err := ti.service.Create(ctx, &gen.CreatePayload{
		Content:          manifest,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	link, err := ti.service.Share(ctx, &gen.SharePayload{SkillID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)

	rr, err := serveSharedSkillRequest(t, ctx, ti.service.ServeSharedSkillMarkdown, link.Token, &customdomains.Context{
		OrganizationID: ti.authContext.ActiveOrganizationID,
		Domain:         "mcp.customer.example",
		DomainID:       uuid.New(),
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rr.Code)
	require.Contains(t, rr.Header().Get("Content-Type"), "text/markdown")
	require.Equal(t, `attachment; filename="SKILL.md"`, rr.Header().Get("Content-Disposition"))
	// The download carries the full manifest, frontmatter included.
	require.Equal(t, manifest, rr.Body.String())
}

// sharedSkillWithFiles creates and shares a skill whose version carries the
// given supporting files.
func sharedSkillWithFiles(t *testing.T, ctx context.Context, ti *testInstance, name string, files map[string][]byte) (manifest, token string) {
	t.Helper()

	manifest = skillManifest(name, "A skill directory.", "# Body")
	created, err := ti.service.Create(ctx, &gen.CreatePayload{Content: manifest, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	for filePath, content := range files {
		require.NoError(t, ti.repo.CreateSkillVersionFile(ctx, repo.CreateSkillVersionFileParams{
			Path: filePath, Content: content, Sha256: "unused", SizeBytes: int64(len(content)), ContentType: "application/octet-stream",
			ProjectID: ti.projectID, SkillID: uuid.MustParse(created.Skill.ID), SkillVersionID: uuid.MustParse(created.Version.ID),
		}))
	}
	link, err := ti.service.Share(ctx, &gen.SharePayload{SkillID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	return manifest, link.Token
}

func TestServeSharedSkillPageListsSupportingFilesAndOffersZip(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	_, token := sharedSkillWithFiles(t, ctx, ti, "page-with-files", map[string][]byte{
		"scripts/run.py":          []byte("print(1)\n"),
		"references/REFERENCE.md": []byte("# Reference\n"),
	})

	rr, err := serveSharedSkillRequest(t, ctx, ti.service.ServeSharedSkillPage, token, &customdomains.Context{
		OrganizationID: ti.authContext.ActiveOrganizationID, Domain: "mcp.customer.example", DomainID: uuid.New(),
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rr.Code)
	body := rr.Body.String()
	require.Contains(t, body, "Supporting files")
	require.Contains(t, body, "scripts/run.py")
	require.Contains(t, body, "references/REFERENCE.md")
	require.Contains(t, body, "/shared/skills/"+token+"/skill.zip")
	require.Contains(t, body, `download="page-with-files.zip"`)
	require.NotContains(t, body, "/shared/skills/"+token+"/SKILL.md")
}

func TestServeSharedSkillArchiveOnCustomDomain(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	files := map[string][]byte{
		"scripts/run.py":  []byte("print(1)\n"),
		"assets/logo.png": {0x89, 'P', 'N', 'G'},
	}
	manifest, token := sharedSkillWithFiles(t, ctx, ti, "zipped-skill", files)

	rr, err := serveSharedSkillRequest(t, ctx, ti.service.ServeSharedSkillArchive, token, &customdomains.Context{
		OrganizationID: ti.authContext.ActiveOrganizationID, Domain: "mcp.customer.example", DomainID: uuid.New(),
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, "application/zip", rr.Header().Get("Content-Type"))
	require.Equal(t, `attachment; filename="zipped-skill.zip"`, rr.Header().Get("Content-Disposition"))
	require.Equal(t, "noindex, nofollow", rr.Header().Get("X-Robots-Tag"))

	zr, err := zip.NewReader(bytes.NewReader(rr.Body.Bytes()), int64(rr.Body.Len()))
	require.NoError(t, err)
	got := make(map[string][]byte, len(zr.File))
	modes := make(map[string]os.FileMode, len(zr.File))
	for _, file := range zr.File {
		entry, err := file.Open()
		require.NoError(t, err)
		content, err := io.ReadAll(entry)
		require.NoError(t, err)
		require.NoError(t, entry.Close())
		got[file.Name] = content
		modes[file.Name] = file.Mode().Perm()
	}
	require.Len(t, got, 3)
	require.Equal(t, []byte(manifest), got["zipped-skill/SKILL.md"])
	require.Equal(t, files["scripts/run.py"], got["zipped-skill/scripts/run.py"])
	require.Equal(t, files["assets/logo.png"], got["zipped-skill/assets/logo.png"])
	require.Equal(t, os.FileMode(0o755), modes["zipped-skill/scripts/run.py"])
	require.Equal(t, os.FileMode(0o644), modes["zipped-skill/assets/logo.png"])
}

func TestServeSharedSkillArchiveWrongOrganizationDomain(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	_, token := sharedSkillWithFiles(t, ctx, ti, "zip-cross-tenant", map[string][]byte{"scripts/run.py": []byte("x")})

	_, err := serveSharedSkillRequest(t, ctx, ti.service.ServeSharedSkillArchive, token, &customdomains.Context{
		OrganizationID: "other-org-" + uuid.NewString(), Domain: "mcp.other.example", DomainID: uuid.New(),
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestServeSharedSkillMarkdownWrongOrganizationDomain(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "raw-cross-tenant", "Raw download must not leak.")
	link, err := ti.service.Share(ctx, &gen.SharePayload{SkillID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)

	_, err = serveSharedSkillRequest(t, ctx, ti.service.ServeSharedSkillMarkdown, link.Token, &customdomains.Context{
		OrganizationID: "other-org-" + uuid.NewString(),
		Domain:         "mcp.other.example",
		DomainID:       uuid.New(),
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}
