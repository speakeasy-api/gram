package skills_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/oops"
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
	require.Contains(t, body, "Powered by Gram")
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
