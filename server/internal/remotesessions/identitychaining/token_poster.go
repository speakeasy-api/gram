package identitychaining

import (
	"context"
	"net/url"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// tokenPoster sends one grant to a token endpoint as its bound client.
type tokenPoster interface {
	Post(ctx context.Context, form url.Values) (remotesessions.TokenResponse, error)
}
