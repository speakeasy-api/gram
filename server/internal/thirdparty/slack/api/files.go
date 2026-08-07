package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	// MaxImageFileBytes caps a single downloaded Slack image at 10 MiB.
	MaxImageFileBytes = 10 * 1024 * 1024

	// imageFetchTimeout bounds the full resolve+download of one file.
	imageFetchTimeout = 30 * time.Second
)

// allowedImageMIMEs is the sniffed-MIME allowlist for downloaded files.
// Slack's declared mimetype is advisory only; the magic-byte sniff decides.
var allowedImageMIMEs = map[string]struct{}{
	"image/png":  {},
	"image/jpeg": {},
	"image/gif":  {},
	"image/webp": {},
}

// ImageFile is a validated image downloaded from Slack.
type ImageFile struct {
	FileID string
	Name   string
	Title  string
	// MimeType is the sniffed content type, not Slack's declared one.
	MimeType string
	Data     []byte
}

// DataURI renders the image as a data: URI suitable for an image_url
// content part.
func (f *ImageFile) DataURI() string {
	return "data:" + f.MimeType + ";base64," + base64.StdEncoding.EncodeToString(f.Data)
}

// FetchImageFile resolves a Slack file ID via files.info and downloads its
// private content with the supplied token. The download URL always comes
// from Slack's files.info response — never from a caller — and its host must
// be a slack.com host, so a forged file record cannot point the server at an
// arbitrary origin. The downloaded bytes are capped at MaxImageFileBytes and
// magic-byte sniffed against an image allowlist.
func (c *Client) FetchImageFile(ctx context.Context, fileID string, token string) (*ImageFile, error) {
	if c.httpClient == nil {
		return nil, fmt.Errorf("slack HTTP client not configured")
	}

	ctx, cancel := context.WithTimeout(ctx, imageFetchTimeout)
	defer cancel()

	body, err := c.CallWithToken(ctx, "files.info", map[string]any{"file": fileID}, token)
	if err != nil {
		return nil, err
	}

	var info struct {
		File struct {
			ID                 string `json:"id"`
			Name               string `json:"name"`
			Title              string `json:"title"`
			Mimetype           string `json:"mimetype"`
			Size               int64  `json:"size"`
			URLPrivateDownload string `json:"url_private_download"`
			URLPrivate         string `json:"url_private"`
		} `json:"file"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("decode files.info response: %w", err)
	}

	if info.File.Size > MaxImageFileBytes {
		return nil, fmt.Errorf("slack file %s is %d bytes, over the %d byte limit", fileID, info.File.Size, MaxImageFileBytes)
	}

	downloadURL := info.File.URLPrivateDownload
	if downloadURL == "" {
		downloadURL = info.File.URLPrivate
	}
	if downloadURL == "" {
		return nil, fmt.Errorf("slack file %s has no private download url", fileID)
	}

	parsed, err := url.Parse(downloadURL)
	if err != nil {
		return nil, fmt.Errorf("parse slack file url: %w", err)
	}
	if err := c.checkFileURL(parsed); err != nil {
		return nil, err
	}

	data, err := c.downloadFile(ctx, parsed.String(), token)
	if err != nil {
		return nil, fmt.Errorf("download slack file %s: %w", fileID, err)
	}

	mime := http.DetectContentType(data)
	if _, ok := allowedImageMIMEs[mime]; !ok {
		return nil, fmt.Errorf("slack file %s content sniffed as %s, not an allowed image type", fileID, mime)
	}

	return &ImageFile{
		FileID:   info.File.ID,
		Name:     info.File.Name,
		Title:    info.File.Title,
		MimeType: mime,
		Data:     data,
	}, nil
}

func (c *Client) downloadFile(ctx context.Context, fileURL string, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get file content: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("file content request returned %d", resp.StatusCode)
	}
	if resp.ContentLength > MaxImageFileBytes {
		return nil, fmt.Errorf("file content is %d bytes, over the %d byte limit", resp.ContentLength, MaxImageFileBytes)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxImageFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read file content: %w", err)
	}
	if len(data) > MaxImageFileBytes {
		return nil, fmt.Errorf("file content exceeds the %d byte limit", MaxImageFileBytes)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("file content is empty")
	}
	return data, nil
}

// checkFileURL validates the resolved download URL before any request is
// made. A client pointed at a non-default API base (tests, Slack-compatible
// proxies) also trusts downloads from that same host — the base URL is set
// by our own code, so the origin that served files.info is a safe origin for
// the content it hands out.
func (c *Client) checkFileURL(u *url.URL) error {
	if c.baseURL != DefaultBaseURL {
		if base, err := url.Parse(c.baseURL); err == nil && base.Host != "" && u.Host == base.Host {
			return nil
		}
	}
	return validateSlackFileURL(u)
}

func validateSlackFileURL(u *url.URL) error {
	if u.Scheme != "https" {
		return fmt.Errorf("slack file url must use https, got %q", u.Scheme)
	}
	host := strings.ToLower(u.Hostname())
	if host != "files.slack.com" && !strings.HasSuffix(host, ".slack.com") {
		return fmt.Errorf("slack file url host %q is not a slack.com host", host)
	}
	return nil
}
