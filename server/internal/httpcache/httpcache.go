// Package httpcache provides a shared write path for public, cacheable JSON
// responses. It centralises the Cache-Control + ETag + conditional-request
// (304) contract so Gram's public well-known / OAuth-metadata responses cache
// consistently.
package httpcache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

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

// strongETag returns a strong ETag (RFC 9110 §8.8.3): a quoted hex SHA-256
// digest of body. Strong because it is computed from the exact bytes Gram
// writes, before any downstream compression. Host-derived URLs are part of body,
// so two hosts of the same resource naturally get distinct ETags without Vary.
func strongETag(body []byte) string {
	sum := sha256.Sum256(body)
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

// ifNoneMatchSatisfied reports whether an If-None-Match header value matches
// etag, using the weak comparison RFC 9110 §13.1.2 mandates for If-None-Match
// (the W/ prefix is ignored on both sides). "*" matches any current
// representation.
func ifNoneMatchSatisfied(header, etag string) bool {
	header = strings.TrimSpace(header)
	switch header {
	case "":
		return false
	case "*":
		return true
	}

	want := strings.TrimPrefix(etag, "W/")
	for candidate := range strings.SplitSeq(header, ",") {
		if strings.TrimPrefix(strings.TrimSpace(candidate), "W/") == want {
			return true
		}
	}
	return false
}
