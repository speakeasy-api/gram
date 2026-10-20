package assets

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/urls"
)

const (
	// OrganizationImageUploadPath receives one image per single-use upload
	// token, sent as "Authorization: Bearer <token>" so it stays out of request
	// logs. It is mounted for PUT beside the dashboard's organization image
	// upload, which takes POST with a session.
	OrganizationImageUploadPath = "/rpc/organizationAssets.uploadImage"

	// OrganizationImageUploadTTL is how long an upload token accepts an image.
	OrganizationImageUploadTTL = 15 * time.Minute

	organizationImageUploadPrefix = "assetsOrganizationImageUpload:"
)

// OrganizationImageUploads issues single-use upload tokens that each store one
// organization-tier image, for clients that cannot send the dashboard's session
// cookie. It keeps the platform uploads' properties: only a hash of each token
// is kept, redeeming it deletes it, and the image type is read from its bytes.
type OrganizationImageUploads struct {
	images  *Service
	cache   cache.Cache
	siteURL *url.URL
	baseURL *url.URL
	logger  *slog.Logger
	now     func() time.Time
}

// NewOrganizationImageUploads issues uploads to baseURL, the API server, and
// reports stored images at their public siteURL address. baseURL must use
// HTTPS, except on a loopback host for local development, because the upload
// token travels in a request header.
func NewOrganizationImageUploads(images *Service, uploadCache cache.Cache, baseURL, siteURL *url.URL, logger *slog.Logger) (*OrganizationImageUploads, error) {
	if baseURL == nil || !urls.IsAbsoluteHTTPSOrLoopback(baseURL.String()) {
		return nil, errors.New("organization image uploads require an HTTPS server URL (HTTP only on a loopback host)")
	}
	return &OrganizationImageUploads{images: images, cache: uploadCache, siteURL: siteURL, baseURL: baseURL, logger: logger.With(attr.SlogComponent("assets")), now: time.Now}, nil
}

type organizationImageUploadGrant struct {
	OrganizationID string    `json:"organization_id"`
	UserID         string    `json:"user_id"`
	ExpiresAt      time.Time `json:"expires_at"`
}

func (g organizationImageUploadGrant) usable(now time.Time) bool {
	return g.OrganizationID != "" && g.UserID != "" && now.Before(g.ExpiresAt)
}

// OrganizationImageUpload is a URL that accepts one image PUT with Token as its
// bearer credential.
type OrganizationImageUpload struct {
	URL       string
	Token     string
	ExpiresAt time.Time
}

// Issue creates a single-use upload token for an image owned by organizationID.
// Callers must already have authorized an org:admin write in that organization
// for userID, who is recorded as the uploader when the URL is used.
func (u *OrganizationImageUploads) Issue(ctx context.Context, organizationID, userID string) (OrganizationImageUpload, error) {
	if organizationID == "" || userID == "" {
		return OrganizationImageUpload{}, errors.New("issue organization image upload: organization and user are required")
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return OrganizationImageUpload{}, fmt.Errorf("issue organization image upload: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(secret)
	grant := organizationImageUploadGrant{OrganizationID: organizationID, UserID: userID, ExpiresAt: u.now().Add(OrganizationImageUploadTTL)}
	if err := u.cache.Set(ctx, organizationImageUploadKey(token), grant, OrganizationImageUploadTTL); err != nil {
		return OrganizationImageUpload{}, fmt.Errorf("store organization image upload: %w", err)
	}
	uploadURL := u.baseURL.ResolveReference(&url.URL{Path: OrganizationImageUploadPath})
	u.logger.InfoContext(ctx, "organization image upload token issued", attr.SlogOrganizationID(organizationID), attr.SlogUserID(userID))
	return OrganizationImageUpload{URL: uploadURL.String(), Token: token, ExpiresAt: grant.ExpiresAt}, nil
}

func organizationImageUploadKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return organizationImageUploadPrefix + hex.EncodeToString(sum[:])
}

// Handler stores the image PUT with an upload token as an organization asset
// and answers with its asset ID and public URL. A token is spent by its first
// request, even one that fails validation.
func (u *OrganizationImageUploads) Handler() http.Handler {
	return imageUploadHandler(u.cache, u.logger, func() time.Time { return u.now() }, u.siteURL, organizationImageUploadKey, func(ctx context.Context, grant organizationImageUploadGrant, data []byte, contentType string) (*slog.Logger, string, string, error) {
		logger := u.logger.With(attr.SlogOrganizationID(grant.OrganizationID), attr.SlogUserID(grant.UserID))
		stored, err := u.images.StoreOrganizationImage(ctx, logger, grant.OrganizationID, grant.UserID, nil, bytes.NewReader(data), int64(len(data)), contentType)
		if err != nil || stored == nil || stored.Asset == nil {
			return logger, "", "", errors.Join(err, errNoStoredImage)
		}
		return logger, stored.Asset.ID, stored.Asset.Sha256, nil
	}, func(ctx context.Context, logger *slog.Logger, assetID uuid.UUID) {
		logger.InfoContext(ctx, "organization image uploaded", attr.SlogAssetID(assetID.String()))
	})
}
