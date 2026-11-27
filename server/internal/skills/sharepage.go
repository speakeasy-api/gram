package skills

import (
	"archive/zip"
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/skills/skilltree"
)

//go:embed shared_skill_page.html.tmpl
var sharedSkillPageTmplData string

var sharedSkillPageTmpl = template.Must(template.New("shared_skill_page").Parse(sharedSkillPageTmplData))

// Token bounds mirror the getShared design payload validation.
const (
	minShareTokenLength = 32
	maxShareTokenLength = 128
)

// sharedSkillArchiveName is the final path segment of a shared skill's ZIP
// download, /shared/skills/{token}/skill.zip. The saved file is named after
// the skill through Content-Disposition.
const sharedSkillArchiveName = "skill.zip"

// The header policy of every shared skill response, the getShared JSON
// endpoint included: short-lived private caching, no search indexing, and no
// referrer leakage of the capability URL.
const (
	sharedSkillCacheControl   = "private, max-age=300"
	sharedSkillXRobotsTag     = "noindex, nofollow"
	sharedSkillReferrerPolicy = "no-referrer"
)

type sharedSkillPageData struct {
	Unavailable  bool
	DisplayName  string
	Summary      string
	UpdatedAt    string
	ContentHTML  template.HTML
	DownloadPath string

	// DownloadName is the file name the browser saves the download under.
	DownloadName string

	// Files lists the skill's supporting files. When it is non-empty the
	// download is a ZIP of the whole skill directory instead of SKILL.md.
	Files []sharedSkillFile
}

// sharedSkillFile is one supporting file as the share page lists it.
type sharedSkillFile struct {
	// Path is the file's path relative to the skill directory.
	Path string

	// Size is the file's size for display, for example "1.2 KB".
	Size string
}

var unavailableSharedSkillPage = sharedSkillPageData{
	Unavailable:  true,
	DisplayName:  "",
	Summary:      "",
	UpdatedAt:    "",
	ContentHTML:  "",
	DownloadPath: "",
	DownloadName: "",
	Files:        nil,
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
		return renderSharedSkillPage(w, http.StatusNotFound, unavailableSharedSkillPage)
	}

	row, err := repo.New(s.db).GetSharedSkillByTokenForOrganization(ctx, repo.GetSharedSkillByTokenForOrganizationParams{
		Token:          token,
		OrganizationID: domainCtx.OrganizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return renderSharedSkillPage(w, http.StatusNotFound, unavailableSharedSkillPage)
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "get shared skill for domain").LogError(ctx, s.logger)
	}

	fileRows, err := repo.New(s.db).ListSkillVersionFileMetadata(ctx, repo.ListSkillVersionFileMetadataParams{
		ProjectID:      row.ProjectID,
		SkillID:        row.SkillID,
		SkillVersionID: row.SkillVersionID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "list shared skill files").LogError(ctx, s.logger)
	}
	files := make([]sharedSkillFile, 0, len(fileRows))
	for _, file := range fileRows {
		files = append(files, sharedSkillFile{Path: file.Path, Size: formatFileSize(file.SizeBytes)})
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

	downloadPath := "/shared/skills/" + token + "/" + skilltree.ManifestPath
	downloadName := skilltree.ManifestPath
	if len(files) > 0 {
		downloadPath = "/shared/skills/" + token + "/" + sharedSkillArchiveName
		downloadName = row.Name + ".zip"
	}

	return renderSharedSkillPage(w, http.StatusOK, sharedSkillPageData{
		Unavailable:  false,
		DisplayName:  row.DisplayName,
		Summary:      conv.PtrValOr(conv.FromPGText[string](row.Summary), ""),
		UpdatedAt:    updatedAt,
		ContentHTML:  contentHTML,
		DownloadPath: downloadPath,
		DownloadName: downloadName,
		Files:        files,
	})
}

// ServeSharedSkillMarkdown serves the raw SKILL.md of a shared skill at
// GET /shared/skills/{token}/SKILL.md with the same tenancy rules as
// ServeSharedSkillPage.
func (s *Service) ServeSharedSkillMarkdown(w http.ResponseWriter, r *http.Request) error {
	row, ok, err := s.sharedSkillForDomain(w, r)
	if err != nil || !ok {
		return err
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

// ServeSharedSkillArchive serves a shared skill's whole directory as a ZIP at
// GET /shared/skills/{token}/skill.zip, with the same tenancy rules as
// ServeSharedSkillPage. The archive holds <name>/SKILL.md and
// <name>/<path> for every supporting file, the layout agents install skills
// from.
func (s *Service) ServeSharedSkillArchive(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	row, ok, err := s.sharedSkillForDomain(w, r)
	if err != nil || !ok {
		return err
	}

	files, err := s.sharedSkillArchiveFiles(ctx, row.ProjectID, row.SkillID, row.SkillVersionID)
	if err != nil {
		return err
	}

	setSharedSkillResponseHeaders(w)
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", sharedSkillArchiveDisposition(row.Name))
	w.WriteHeader(http.StatusOK)
	// The archive streams straight to the response. The files were validated
	// above, so the only failure left is writing to the client.
	if err := writeSkillArchive(w, row.Name, row.Content, files); err != nil {
		return fmt.Errorf("write shared skill archive: %w", err)
	}

	return nil
}

// DownloadShared serves a shared skill's whole directory as a ZIP through
// /rpc/skills.downloadShared, the platform-host counterpart of
// ServeSharedSkillArchive that the dashboard share page links to. Like
// GetShared the share token is the only credential.
func (s *Service) DownloadShared(ctx context.Context, payload *gen.DownloadSharedPayload) (*gen.DownloadSharedResult, io.ReadCloser, error) {
	row, err := repo.New(s.db).GetSharedSkillByToken(ctx, payload.Token)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, oops.E(oops.CodeNotFound, nil, "link not found or no longer available")
	}
	if err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, err, "get shared skill").LogError(ctx, s.logger)
	}

	files, err := s.sharedSkillArchiveFiles(ctx, row.ProjectID, row.SkillID, row.SkillVersionID)
	if err != nil {
		return nil, nil, err
	}

	// The archive streams to the response through a pipe rather than being
	// buffered a second time next to the file contents. The writer stops when
	// the response body is closed.
	pr, pw := io.Pipe()
	go func() {
		pw.CloseWithError(writeSkillArchive(pw, row.Name, row.Content, files))
	}()

	return &gen.DownloadSharedResult{
		ContentType:        "application/zip",
		ContentDisposition: sharedSkillArchiveDisposition(row.Name),
		CacheControl:       sharedSkillCacheControl,
		XRobotsTag:         sharedSkillXRobotsTag,
		ReferrerPolicy:     sharedSkillReferrerPolicy,
	}, pr, nil
}

// sharedSkillFiles returns a shared version's supporting files and, when it
// has any, the server path of its ZIP download.
func (s *Service) sharedSkillFiles(ctx context.Context, projectID, skillID, versionID uuid.UUID, token string) ([]*types.SkillFile, *string, error) {
	rows, err := repo.New(s.db).ListSkillVersionFileMetadata(ctx, repo.ListSkillVersionFileMetadataParams{
		ProjectID:      projectID,
		SkillID:        skillID,
		SkillVersionID: versionID,
	})
	if err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, err, "list shared skill files").LogError(ctx, s.logger)
	}

	files := make([]*types.SkillFile, 0, len(rows))
	for _, row := range rows {
		files = append(files, &types.SkillFile{
			Path:        row.Path,
			Sha256:      row.Sha256,
			SizeBytes:   row.SizeBytes,
			ContentType: row.ContentType,
		})
	}
	if len(files) == 0 {
		return files, nil, nil
	}
	archivePath := "/rpc/skills.downloadShared?" + url.Values{"token": []string{token}}.Encode()
	return files, &archivePath, nil
}

// sharedSkillArchiveFiles loads the supporting files of a shared skill
// version for its ZIP. A stored path that fails validation fails the download
// rather than producing an archive missing a file the share page lists.
func (s *Service) sharedSkillArchiveFiles(ctx context.Context, projectID, skillID, versionID uuid.UUID) ([]repo.ListSkillVersionFilesRow, error) {
	files, err := repo.New(s.db).ListSkillVersionFiles(ctx, repo.ListSkillVersionFilesParams{
		ProjectID:      projectID,
		SkillID:        skillID,
		SkillVersionID: versionID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list shared skill files").LogError(ctx, s.logger)
	}
	for _, file := range files {
		if err := skilltree.ValidatePath(file.Path); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "invalid shared skill file path").LogError(ctx, s.logger)
		}
	}
	return files, nil
}

func sharedSkillArchiveDisposition(name string) string {
	return fmt.Sprintf("attachment; filename=%q", name+".zip")
}

// sharedSkillForDomain resolves the shared skill a download request names,
// pinned to the serving custom domain's organization. On the platform host it
// redirects to the dashboard route and reports ok false with no error.
func (s *Service) sharedSkillForDomain(w http.ResponseWriter, r *http.Request) (row repo.GetSharedSkillByTokenForOrganizationRow, ok bool, err error) {
	ctx := r.Context()
	token := chi.URLParam(r, "token")

	domainCtx := customdomains.FromContext(ctx)
	if domainCtx == nil {
		http.Redirect(w, r, s.siteURL.JoinPath("shared", "skills", token).String(), http.StatusFound)
		return row, false, nil
	}

	if len(token) < minShareTokenLength || len(token) > maxShareTokenLength {
		return row, false, oops.E(oops.CodeNotFound, nil, "link not found or no longer available")
	}

	row, err = repo.New(s.db).GetSharedSkillByTokenForOrganization(ctx, repo.GetSharedSkillByTokenForOrganizationParams{
		Token:          token,
		OrganizationID: domainCtx.OrganizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return row, false, oops.E(oops.CodeNotFound, nil, "link not found or no longer available")
	}
	if err != nil {
		return row, false, oops.E(oops.CodeUnexpected, err, "get shared skill for domain").LogError(ctx, s.logger)
	}
	return row, true, nil
}

// writeSkillArchive writes a skill directory as a deterministic ZIP rooted at
// a directory named after the skill. Files under scripts/ are marked
// executable, since the Agent Skills specification puts runnable code there.
// A file whose path fails validation is an error, never silently left out.
func writeSkillArchive(w io.Writer, name, manifest string, files []repo.ListSkillVersionFilesRow) error {
	zw := zip.NewWriter(w)
	if err := writeSkillArchiveEntry(zw, path.Join(name, skilltree.ManifestPath), []byte(manifest), 0o644); err != nil {
		return err
	}
	for _, file := range files {
		if err := skilltree.ValidatePath(file.Path); err != nil {
			return fmt.Errorf("skill archive entry: %w", err)
		}
		var mode os.FileMode = 0o644
		if skilltree.IsScript(file.Path) {
			mode = 0o755
		}
		if err := writeSkillArchiveEntry(zw, path.Join(name, file.Path), file.Content, mode); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("close skill archive: %w", err)
	}
	return nil
}

func writeSkillArchiveEntry(zw *zip.Writer, name string, content []byte, mode os.FileMode) error {
	header := &zip.FileHeader{
		Name:               name,
		Comment:            "",
		NonUTF8:            false,
		CreatorVersion:     0,
		ReaderVersion:      0,
		Flags:              0,
		Method:             zip.Deflate,
		Modified:           time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC),
		ModifiedTime:       0,
		ModifiedDate:       0,
		CRC32:              0,
		CompressedSize:     0,
		UncompressedSize:   0,
		CompressedSize64:   0,
		UncompressedSize64: 0,
		Extra:              nil,
		ExternalAttrs:      0,
	}
	header.SetMode(mode)
	f, err := zw.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("create skill archive entry %q: %w", name, err)
	}
	if _, err := f.Write(content); err != nil {
		return fmt.Errorf("write skill archive entry %q: %w", name, err)
	}
	return nil
}

// formatFileSize renders a byte count for display with binary units.
func formatFileSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	value, suffix := float64(size)/unit, "KB"
	// Compare after rounding to one decimal, so a size just under 1 MB shows
	// as "1.0 MB" rather than "1024.0 KB".
	if value >= unit-0.05 {
		value, suffix = value/unit, "MB"
	}
	return fmt.Sprintf("%.1f %s", value, suffix)
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

// setSharedSkillResponseHeaders applies the shared skill header policy to a
// response the service writes directly.
func setSharedSkillResponseHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", sharedSkillCacheControl)
	w.Header().Set("X-Robots-Tag", sharedSkillXRobotsTag)
	w.Header().Set("Referrer-Policy", sharedSkillReferrerPolicy)
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
