package assets

import (
	"net/url"

	"github.com/google/uuid"
)

// ServeImageURL returns the public assets.serveImage URL for an image asset.
// base is cloned, never mutated.
func ServeImageURL(base *url.URL, assetID uuid.UUID) string {
	u := *base
	u.Path = "/rpc/assets.serveImage"
	q := u.Query()
	q.Set("id", assetID.String())
	u.RawQuery = q.Encode()
	return u.String()
}
