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
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const (
	// PlatformImageUploadPath receives one image on a signed upload URL. It is
	// mounted on the admin server beside the dashboard's platform image upload.
	PlatformImageUploadPath = "/admin/assets.uploadImage/{token}"

	// PlatformImageUploadTTL is how long a signed upload URL accepts an image.
	PlatformImageUploadTTL = 15 * time.Minute

	platformImageUploadPrefix = "assetsPlatformImageUpload:"
)

// PlatformImageTypes are the image types accepted for platform-tier assets.
// SVG is excluded because it can carry script.
var PlatformImageTypes = []string{"image/png", "image/jpeg", "image/gif", "image/webp"}

// PlatformImageUploads issues single-use signed URLs that store one platform
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

// NewPlatformImageUploads issues upload URLs on baseURL, the admin server, and
// reports stored images at their public siteURL address.
func NewPlatformImageUploads(images *Service, uploadCache cache.Cache, baseURL, siteURL *url.URL, logger *slog.Logger) *PlatformImageUploads {
	return &PlatformImageUploads{images: images, cache: uploadCache, siteURL: siteURL, baseURL: baseURL, logger: logger.With(attr.SlogComponent("assets")), now: time.Now}
}

type platformImageUploadGrant struct {
	OperatorEmail string    `json:"operator_email"`
	ExpiresAt     time.Time `json:"expires_at"`
}

// PlatformImageUpload is a signed URL that accepts one image.
type PlatformImageUpload struct {
	URL       string
	ExpiresAt time.Time
}

// StoredPlatformImage describes an image stored through a signed upload URL.
type StoredPlatformImage struct {
	AssetID     string `json:"asset_id"`
	URL         string `json:"url"`
	SHA256      string `json:"sha256"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}

// Issue creates a signed upload URL. Callers must already have authorized a
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
	upload := *u.baseURL
	upload.Path = "/admin/assets.uploadImage/" + token
	upload.RawQuery = ""
	return PlatformImageUpload{URL: upload.String(), ExpiresAt: grant.ExpiresAt}, nil
}

func platformImageUploadKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return platformImageUploadPrefix + hex.EncodeToString(sum[:])
}

// Handler stores the image PUT to a signed upload URL and answers with its
// asset ID and public URL. A URL is spent by its first request, even one that
// fails validation.
func (u *PlatformImageUploads) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodPut {
			w.Header().Set("Allow", http.MethodPut)
			writeUploadJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "use PUT"})
			return
		}
		var grant platformImageUploadGrant
		if err := u.cache.GetAndDelete(r.Context(), platformImageUploadKey(r.PathValue("token")), &grant); err != nil || grant.OperatorEmail == "" || !u.now().Before(grant.ExpiresAt) {
			writeUploadJSON(w, http.StatusNotFound, map[string]string{"error": "this upload URL is unknown, used or expired; request a new one"})
			return
		}
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxFileSizeImage))
		if err != nil {
			writeUploadJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "images are limited to 4 MiB"})
			return
		}
		// Trust the bytes, not the request's Content-Type: curl sends a form
		// type by default.
		contentType := http.DetectContentType(data)
		if len(data) == 0 || !slices.Contains(PlatformImageTypes, contentType) {
			writeUploadJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "upload a PNG, JPEG, GIF or WebP image"})
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
			logger.ErrorContext(r.Context(), "signed platform image upload failed", attr.SlogError(err))
			writeUploadJSON(w, http.StatusInternalServerError, map[string]string{"error": "upload failed"})
			return
		}
		assetID, err := uuid.Parse(stored.Asset.ID)
		if err != nil {
			writeUploadJSON(w, http.StatusInternalServerError, map[string]string{"error": "upload failed"})
			return
		}
		writeUploadJSON(w, http.StatusOK, StoredPlatformImage{AssetID: assetID.String(), URL: ServeImageURL(u.siteURL, assetID), SHA256: stored.Asset.Sha256, ContentType: contentType, Size: int64(len(data))})
	})
}

func writeUploadJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
