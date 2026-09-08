// Package httpcache holds shared HTTP caching semantics, in both directions.
// The write path (WriteCacheableJSON) centralises the Cache-Control + ETag +
// conditional-request (304) contract so public well-known / OAuth-metadata
// responses cache consistently. The read path (FreshnessPolicy, SanitizeETag)
// parses the same headers off responses fetched from other hosts, so every
// remote-document cache derives freshness and revalidation state identically.
// Header-specific parsing lives in the http_header_*.go files, one per header
// family.
package httpcache

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

// WriteCacheableJSON writes body as a public, cacheable JSON response. It always
// emits a strong ETag derived from body and Cache-Control: public,
// max-age=<maxAge>, and honours a matching If-None-Match by returning 304 Not
// Modified with no body. contentType is emitted verbatim so callers can vary
// the charset parameter.
//
// Callers must not have written to w yet and must already hold the marshalled
// body: like the metadata writers it replaces, this commits the status line, so
// any marshalling or resolution error must be returned earlier on an unwritten
// ResponseWriter for the error-handling middleware to emit the real status.
func WriteCacheableJSON(ctx context.Context, w http.ResponseWriter, r *http.Request, logger *slog.Logger, contentType string, maxAge int, body []byte) error {
	etag := strongETag(body)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", maxAge))

	if ifNoneMatchSatisfied(r.Header.Get("If-None-Match"), etag) {
		// RFC 9110 §15.4.5: a 304 carries the validators (ETag, Cache-Control)
		// already set above but no representation, so Content-Type and the body
		// are omitted.
		w.WriteHeader(http.StatusNotModified)
		return nil
	}

	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(body); err != nil {
		return oops.E(oops.CodeUnexpected, err, "write cacheable response body").LogError(ctx, logger)
	}

	return nil
}
