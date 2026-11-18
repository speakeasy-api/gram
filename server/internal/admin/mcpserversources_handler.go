package admin

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/assets/blobio"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// SetMCPSourceStorage enables staff downloads from the same asset storage used
// for project deployments. Storage URLs are never accepted from a request.
func (s *Service) SetMCPSourceStorage(storage blobio.Reader) { s.mcpSourceStorage = storage }

func (s *Service) mcpServerSourcesHandler(download bool) http.Handler {
	return oops.ErrHandle(s.logger, func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Cache-Control", "private, no-store")
		ctx, err := s.authorizeAdminRequest(r)
		if err != nil {
			return err
		}
		query := r.URL.Query()
		organizationID := query.Get("organization_id")
		if strings.TrimSpace(organizationID) == "" {
			return oops.C(oops.CodeBadRequest)
		}
		projectID, err := uuid.Parse(query.Get("project_id"))
		if err != nil || projectID == uuid.Nil {
			return oops.C(oops.CodeBadRequest)
		}
		serverID, err := uuid.Parse(query.Get("mcp_server_id"))
		if err != nil || serverID == uuid.Nil {
			return oops.C(oops.CodeBadRequest)
		}
		var assetID uuid.UUID
		if download {
			assetID, err = uuid.Parse(query.Get("asset_id"))
			if err != nil || assetID == uuid.Nil {
				return oops.C(oops.CodeBadRequest)
			}
		}
		sources, err := s.projectMCPServerSources(ctx, organizationID, projectID, serverID)
		if err != nil {
			return err
		}
		if download {
			for _, source := range sources {
				if source.AssetID == assetID {
					return s.downloadMCPServerSource(ctx, w, source)
				}
			}
			return oops.C(oops.CodeNotFound)
		}
		type sourceView struct {
			AssetID  uuid.UUID `json:"asset_id"`
			Name     string    `json:"name"`
			Filename string    `json:"filename"`
		}
		result := struct {
			Sources []sourceView `json:"sources"`
		}{Sources: make([]sourceView, 0, len(sources))}
		for _, source := range sources {
			result.Sources = append(result.Sources, sourceView{AssetID: source.AssetID, Name: source.Name, Filename: source.downloadFilename()})
		}
		w.Header().Set("Content-Type", "application/json")
		return json.NewEncoder(w).Encode(result)
	})
}

func (s *Service) downloadMCPServerSource(ctx context.Context, w http.ResponseWriter, source adminOpenAPISource) error {
	if s.mcpSourceStorage == nil {
		return oops.C(oops.CodeUnavailable)
	}
	assetURL, err := url.Parse(source.URL)
	if err != nil || assetURL.Scheme == "" {
		return oops.E(oops.CodeUnexpected, err, "invalid source storage URL")
	}
	body, err := s.mcpSourceStorage.Read(ctx, assetURL)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "open source asset")
	}
	defer o11y.LogDefer(ctx, s.logger, "close source asset", body.Close)
	// Treat customer-provided sources as downloads, never active browser content.
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": source.downloadFilename()}))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox")
	if _, err := io.Copy(w, body); err != nil {
		s.logger.ErrorContext(ctx, "stream source asset failed", "error", err)
		// Headers may already be committed: abort rather than append a JSON error
		// to a truncated attachment and make it look like a successful download.
		panic(http.ErrAbortHandler)
	}
	return nil
}

func (source adminOpenAPISource) downloadFilename() string {
	filename := path.Base(strings.ReplaceAll(source.Name, "\\", "/"))
	filename = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, filename)
	if filename == "" || filename == "." || filename == "/" || filename == ".." {
		filename = source.AssetID.String()
	}
	switch strings.ToLower(path.Ext(filename)) {
	case ".yaml", ".yml", ".json":
		return filename
	}
	mediaType, _, _ := mime.ParseMediaType(source.ContentType)
	if mediaType == "application/json" || mediaType == "text/json" {
		return filename + ".json"
	}
	return filename + ".yaml"
}
