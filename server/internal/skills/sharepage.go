package skills

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
)

//go:embed shared_skill_page.html.tmpl
var sharedSkillPageTmplData string

var sharedSkillPageTmpl = template.Must(template.New("shared_skill_page").Parse(sharedSkillPageTmplData))

// Token bounds mirror the getShared design payload validation.
const (
	minShareTokenLength = 32
	maxShareTokenLength = 128
)

type sharedSkillPageData struct {
	Unavailable  bool
	DisplayName  string
	Summary      string
	UpdatedAt    string
	ContentHTML  template.HTML
	DownloadPath string
}

// ServeSharedSkillPage renders the public share page for a skill at
// GET /shared/skills/{token}. On a custom domain the lookup is pinned to the
// domain's organization so one tenant's skill can never be served under
// another tenant's domain. On the platform host the canonical page is the
// dashboard SPA route, so the request is redirected there instead.
func (s *Service) ServeSharedSkillPage(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	token := chi.URLParam(r, "token")

	domainCtx := customdomains.FromContext(ctx)
	if domainCtx == nil {
		http.Redirect(w, r, s.siteURL.JoinPath("shared", "skills", token).String(), http.StatusFound)
		return nil
	}

	if len(token) < minShareTokenLength || len(token) > maxShareTokenLength {
		return renderSharedSkillPage(w, http.StatusNotFound, sharedSkillPageData{
			Unavailable:  true,
			DisplayName:  "",
			Summary:      "",
			UpdatedAt:    "",
			ContentHTML:  "",
			DownloadPath: "",
		})
	}

	row, err := repo.New(s.db).GetSharedSkillByTokenForOrganization(ctx, repo.GetSharedSkillByTokenForOrganizationParams{
		Token:          token,
		OrganizationID: domainCtx.OrganizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return renderSharedSkillPage(w, http.StatusNotFound, sharedSkillPageData{
			Unavailable:  true,
			DisplayName:  "",
			Summary:      "",
			UpdatedAt:    "",
			ContentHTML:  "",
			DownloadPath: "",
		})
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "get shared skill for domain").LogError(ctx, s.logger)
	}

	body := stripSkillFrontmatter(row.Content)
	var contentHTML template.HTML
	if strings.TrimSpace(body) != "" {
		rendered, err := conv.MarkdownToHTML([]byte(body))
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "render shared skill markdown").LogError(ctx, s.logger)
		}
		contentHTML = template.HTML(rendered) // #nosec G203 // sanitized by bluemonday in conv.MarkdownToHTML
	}

	var updatedAt string
	if row.VersionCreatedAt.Valid {
		updatedAt = row.VersionCreatedAt.Time.UTC().Format("January 2, 2006")
	}

	return renderSharedSkillPage(w, http.StatusOK, sharedSkillPageData{
		Unavailable:  false,
		DisplayName:  row.DisplayName,
		Summary:      conv.PtrValOr(conv.FromPGText[string](row.Summary), ""),
		UpdatedAt:    updatedAt,
		ContentHTML:  contentHTML,
		DownloadPath: "/shared/skills/" + token + "/SKILL.md",
	})
}

// ServeSharedSkillMarkdown serves the raw SKILL.md of a shared skill at
// GET /shared/skills/{token}/SKILL.md with the same tenancy rules as
// ServeSharedSkillPage.
func (s *Service) ServeSharedSkillMarkdown(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	token := chi.URLParam(r, "token")

	domainCtx := customdomains.FromContext(ctx)
	if domainCtx == nil {
		http.Redirect(w, r, s.siteURL.JoinPath("shared", "skills", token).String(), http.StatusFound)
		return nil
	}

	if len(token) < minShareTokenLength || len(token) > maxShareTokenLength {
		return oops.E(oops.CodeNotFound, nil, "link not found or no longer available")
	}

	row, err := repo.New(s.db).GetSharedSkillByTokenForOrganization(ctx, repo.GetSharedSkillByTokenForOrganizationParams{
		Token:          token,
		OrganizationID: domainCtx.OrganizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return oops.E(oops.CodeNotFound, nil, "link not found or no longer available")
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "get shared skill for domain").LogError(ctx, s.logger)
	}

	setSharedSkillResponseHeaders(w)
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="SKILL.md"`)
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(row.Content)); err != nil {
		return fmt.Errorf("write shared skill markdown: %w", err)
	}

	return nil
}

func renderSharedSkillPage(w http.ResponseWriter, status int, data sharedSkillPageData) error {
	var buf bytes.Buffer
	if err := sharedSkillPageTmpl.Execute(&buf, data); err != nil {
		return oops.E(oops.CodeUnexpected, err, "render shared skill page")
	}

	setSharedSkillResponseHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src https: data:; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	w.WriteHeader(status)
	if _, err := w.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("write shared skill page: %w", err)
	}

	return nil
}

// setSharedSkillResponseHeaders mirrors the header policy of the
// /rpc/skills.getShared JSON endpoint: short-lived private caching, no search
// indexing, and no referrer leakage of the capability URL.
func setSharedSkillResponseHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("Referrer-Policy", "no-referrer")
}

// stripSkillFrontmatter removes the leading YAML frontmatter block from a
// SKILL.md manifest, mirroring the dashboard's stripSkillFrontmatter helper so
// the public page shows only the Markdown body.
func stripSkillFrontmatter(content string) string {
	const utf8BOM = "\ufeff"
	normalized := strings.TrimPrefix(content, utf8BOM)
	normalized = strings.ReplaceAll(normalized, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	lines := strings.Split(normalized, "\n")

	if strings.TrimRight(lines[0], " \t") != "---" {
		return content
	}

	closing := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], " \t") == "---" {
			closing = i
			break
		}
	}
	if closing < 0 {
		return content
	}

	return strings.TrimPrefix(strings.Join(lines[closing+1:], "\n"), "\n")
}
