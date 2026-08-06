package assets

import (
	"net/url"

	"github.com/google/uuid"
)

// ServeImageURL returns the public URL that serves the given image asset
// through the unauthenticated assets.serveImage endpoint. base — typically
// the configured Gram server URL — is cloned, never mutated.
func ServeImageURL(base *url.URL, assetID uuid.UUID) string {
	u := *base
	u.Path = "/rpc/assets.serveImage"
	q := u.Query()
	q.Set("id", assetID.String())
	u.RawQuery = q.Encode()
	return u.String()
}
