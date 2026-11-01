package assets

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/mcp/httpheaders"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urls"
)

const (
	// PlatformImageUploadPath receives one image per single-use upload token,
	// sent as "Authorization: Bearer <token>" so it stays out of request logs.
	// It is mounted on the admin server beside the dashboard's platform image
	// upload, and the admin Origin check exempts it because the token, not a
	// browser cookie, authenticates the request.
	PlatformImageUploadPath = "/admin/assets.uploadImage"

	// PlatformImageUploadTTL is how long an upload token accepts an image.
	PlatformImageUploadTTL = 15 * time.Minute

	platformImageUploadPrefix = "assetsPlatformImageUpload:"
)

// PlatformImageTypes are the image types accepted for platform-tier assets.
// SVG is accepted only after validation of the static subset.
var PlatformImageTypes = []string{"image/png", "image/jpeg", "image/gif", "image/webp", "image/svg+xml"}

// PlatformImageUploads issues single-use upload tokens that store one platform
// image each, for clients that cannot send the dashboard's session cookie.
// Only a hash of each token is kept, and redeeming it deletes it.
type PlatformImageUploads struct {
	images  *Service
	cache   cache.Cache
	siteURL *url.URL
	baseURL *url.URL
	logger  *slog.Logger
	now     func() time.Time
}

// NewPlatformImageUploads issues uploads to baseURL, the admin server, and
// reports stored images at their public siteURL address. baseURL must use
// HTTPS, except on a loopback host for local development, because the upload
// token travels in a request header.
func NewPlatformImageUploads(images *Service, uploadCache cache.Cache, baseURL, siteURL *url.URL, logger *slog.Logger) (*PlatformImageUploads, error) {
	if baseURL == nil || !urls.IsAbsoluteHTTPSOrLoopback(baseURL.String()) {
		return nil, errors.New("platform image uploads require an HTTPS admin server URL (HTTP only on a loopback host)")
	}
	if siteURL == nil || !urls.IsAbsoluteHTTPSOrLoopback(siteURL.String()) {
		return nil, errors.New("platform image uploads require an HTTPS public site URL (HTTP only on a loopback host)")
	}
	return &PlatformImageUploads{images: images, cache: uploadCache, siteURL: siteURL, baseURL: baseURL, logger: logger.With(attr.SlogComponent("assets")), now: time.Now}, nil
}

type platformImageUploadGrant struct {
	OperatorEmail string    `json:"operator_email"`
	ExpiresAt     time.Time `json:"expires_at"`
}

// PlatformImageUpload is a URL that accepts one image PUT with Token as its
// bearer credential.
type PlatformImageUpload struct {
	URL       string
	Token     string
	ExpiresAt time.Time
}

// StoredPlatformImage describes an image stored with an upload token.
type StoredPlatformImage struct {
	AssetID     string `json:"asset_id"`
	URL         string `json:"url"`
	SHA256      string `json:"sha256"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}

// Issue creates a single-use upload token. Callers must already have authorized a
// platform-wide staff write for operatorEmail, which is recorded as the
// uploader when the URL is used.
func (u *PlatformImageUploads) Issue(ctx context.Context, operatorEmail string) (PlatformImageUpload, error) {
	if operatorEmail == "" {
		return PlatformImageUpload{}, errors.New("issue platform image upload: operator email is required")
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return PlatformImageUpload{}, fmt.Errorf("issue platform image upload: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(secret)
	grant := platformImageUploadGrant{OperatorEmail: operatorEmail, ExpiresAt: u.now().Add(PlatformImageUploadTTL)}
	if err := u.cache.Set(ctx, platformImageUploadKey(token), grant, PlatformImageUploadTTL); err != nil {
		return PlatformImageUpload{}, fmt.Errorf("store platform image upload: %w", err)
	}
	uploadURL := u.baseURL.ResolveReference(&url.URL{Path: PlatformImageUploadPath})
	u.logger.InfoContext(ctx, "staff platform image upload token issued", attr.SlogAuthUserEmail(operatorEmail))
	return PlatformImageUpload{URL: uploadURL.String(), Token: token, ExpiresAt: grant.ExpiresAt}, nil
}

func platformImageUploadKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return platformImageUploadPrefix + hex.EncodeToString(sum[:])
}

// Handler stores the image PUT with an upload token and answers with its asset
// ID and public URL. A token is spent by its first request, even one that fails
// validation.
func (u *PlatformImageUploads) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodPut {
			w.Header().Set("Allow", http.MethodPut)
			writeUploadJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "use PUT"})
			return
		}
		token := httpheaders.AuthorizationBearerToken(r)
		if token == "" {
			writeUploadJSON(w, http.StatusUnauthorized, map[string]string{"error": "send the upload token as Authorization: Bearer <token>"})
			return
		}
		var grant platformImageUploadGrant
		err := u.cache.GetAndDelete(r.Context(), platformImageUploadKey(token), &grant)
		if err != nil && !errors.Is(err, redisCache.ErrCacheMiss) {
			u.logger.ErrorContext(r.Context(), "redeem platform image upload token", attr.SlogError(err))
			writeUploadJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "uploads are temporarily unavailable; try again"})
			return
		}
		if errors.Is(err, redisCache.ErrCacheMiss) || grant.OperatorEmail == "" || !u.now().Before(grant.ExpiresAt) {
			writeUploadJSON(w, http.StatusUnauthorized, map[string]string{"error": "this upload token is unknown, used or expired; request a new one"})
			return
		}
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxFileSizeImage))
		var tooLarge *http.MaxBytesError
		switch {
		case errors.As(err, &tooLarge):
			writeUploadJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "images are limited to 4 MiB"})
			return
		case err != nil:
			writeUploadJSON(w, http.StatusBadRequest, map[string]string{"error": "the upload body could not be read; request a new token and try again"})
			return
		}
		if r.ContentLength >= 0 && r.ContentLength != int64(len(data)) {
			writeUploadJSON(w, http.StatusBadRequest, map[string]string{"error": "image content length mismatch"})
			return
		}
		// Trust the bytes, not the request's Content-Type: curl sends a form
		// type by default.
		contentType := http.DetectContentType(data)
		// Explicit SVG must not disguise raster bytes; other MIME declarations
		// remain advisory for shell clients.
		if declared, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); declared == "image/svg+xml" {
			contentType = declared
		}
		data, contentType, err = canonicalPlatformImage(data, contentType)
		if err != nil || len(data) == 0 || !slices.Contains(PlatformImageTypes, contentType) {
			writeUploadJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "upload a PNG, JPEG, GIF, WebP or static SVG image"})
			return
		}
		logger := u.logger.With(attr.SlogAuthUserEmail(grant.OperatorEmail))
		stored, err := u.images.StorePlatformImage(r.Context(), logger, grant.OperatorEmail, bytes.NewReader(data), int64(len(data)), contentType)
		if err != nil || stored == nil || stored.Asset == nil {
			var shareable *oops.ShareableError
			if errors.As(err, &shareable) && shareable.Code != oops.CodeUnexpected {
				writeUploadJSON(w, http.StatusBadRequest, map[string]string{"error": shareable.Error()})
				return
			}
			logger.ErrorContext(r.Context(), "platform image upload failed", attr.SlogError(err))
			writeUploadJSON(w, http.StatusInternalServerError, map[string]string{"error": "upload failed"})
			return
		}
		assetID, err := uuid.Parse(stored.Asset.ID)
		if err != nil {
			writeUploadJSON(w, http.StatusInternalServerError, map[string]string{"error": "upload failed"})
			return
		}
		// The staff record of this direct write: who published which image.
		logger.InfoContext(r.Context(), "staff platform image uploaded", attr.SlogAssetID(assetID.String()))
		writeUploadJSON(w, http.StatusOK, StoredPlatformImage{AssetID: assetID.String(), URL: ServeImageURL(u.siteURL, assetID), SHA256: stored.Asset.Sha256, ContentType: stored.Asset.ContentType, Size: stored.Asset.ContentLength})
	})
}

func writeUploadJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
