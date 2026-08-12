package slack

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
)

// FileProxy serves the short-lived download URLs minted by
// platform_slack_get_file_url. The sealed token in the query string is the
// only credential: it authenticates the request, names the file, and carries
// the Slack token for the upstream download. Bytes stream through per
// request and are never stored.
type FileProxy struct {
	logger *slog.Logger
	enc    *encryption.Client
	client *apiClient
}

func NewFileProxy(logger *slog.Logger, enc *encryption.Client, httpClient *guardian.HTTPClient) *FileProxy {
	return &FileProxy{
		logger: logger,
		enc:    enc,
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
	}
}

func (p *FileProxy) Attach(mux interface {
	Handle(string, string, http.HandlerFunc)
}) {
	mux.Handle("GET", "/platform/slack/files", p.serveFile)
}

func (p *FileProxy) serveFile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	blob := r.URL.Query().Get("t")
	if blob == "" || p.enc == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	plaintext, err := p.enc.Decrypt(blob)
	if err != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var sealed sealedFileFetch
	if err := json.Unmarshal([]byte(plaintext), &sealed); err != nil || sealed.FileID == "" || sealed.Token == "" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if time.Now().Unix() > sealed.ExpiresAt {
		http.Error(w, "expired", http.StatusForbidden)
		return
	}

	img, err := p.client.FetchImageFile(ctx, sealed.FileID, sealed.Token)
	if err != nil {
		p.logger.WarnContext(ctx, "serve slack file: fetch failed", attr.SlogError(err))
		http.Error(w, "fetch failed", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", img.MimeType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", "inline")
	if _, err := w.Write(img.Data); err != nil {
		p.logger.WarnContext(ctx, "serve slack file: write response", attr.SlogError(err))
	}
}
