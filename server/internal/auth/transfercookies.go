package auth

import (
	"context"
	"net/http"

	"github.com/speakeasy-api/gram/server/internal/constants"
)

// TransferCookieJar reads and writes a browser's session transfer nonce
// cookies. Each transfer has its own cookie, named from its nonce hash, so
// two transfers started in one browser do not overwrite each other.
type TransferCookieJar interface {
	Get(name string) string
	Set(name, value string)
	Clear(name string)
}

type transferCookieJarKey struct{}

// WithTransferCookieJar gives TransferIn the browser's transfer cookies. Exported for use in tests only; requests get theirs from
// transferCookieMiddleware.
func WithTransferCookieJar(ctx context.Context, jar TransferCookieJar) context.Context {
	return context.WithValue(ctx, transferCookieJarKey{}, jar)
}

func transferCookieJarFromContext(ctx context.Context) (TransferCookieJar, bool) {
	jar, ok := ctx.Value(transferCookieJarKey{}).(TransferCookieJar)
	return jar, ok
}

// transferNonceCookieName names the cookie of the transfer whose nonce hash
// is nonceHash. Sixteen hex characters keep the name short and unique enough
// for the few transfers one browser holds at a time.
func transferNonceCookieName(nonceHash string) string {
	return constants.TransferInNonceCookiePrefix + nonceHash[:min(16, len(nonceHash))]
}

// transferCookieMiddleware hands the request's cookies and the response to
// the wrapped TransferIn handler.
func transferCookieMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(WithTransferCookieJar(r.Context(), httpTransferCookieJar{w: w, r: r})))
	})
}

type httpTransferCookieJar struct {
	w http.ResponseWriter
	r *http.Request
}

func (j httpTransferCookieJar) Get(name string) string {
	c, err := j.r.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}

func (j httpTransferCookieJar) Set(name, value string) {
	j.set(name, value, constants.TransferInNonceCookieMaxAgeSeconds)
}

func (j httpTransferCookieJar) Clear(name string) {
	// A negative MaxAge expires the cookie now; zero would leave it unset.
	j.set(name, "", -1)
}

// set writes a host-only cookie. The __Host- prefix requires Secure, Path=/
// and no Domain; browsers accept Secure cookies on http://localhost too.
func (j httpTransferCookieJar) set(name, value string, maxAge int) {
	//nolint:exhaustruct // only these fields matter; the rest stay unset
	http.SetCookie(j.w, &http.Cookie{
		Name:     name,
		Value:    value,
		MaxAge:   maxAge,
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}
