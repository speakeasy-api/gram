package httputil

import (
	"mime"
	"net/http"
	"strings"
)

// IsBrowserPageRequest checks if the request is from a browser by examining the Accept header
// for HTML content types (text/html or application/xhtml+xml).
func IsBrowserPageRequest(r *http.Request) bool {
	for mediaTypeFull := range strings.SplitSeq(r.Header.Get("Accept"), ",") {
		if mediatype, _, err := mime.ParseMediaType(mediaTypeFull); err == nil &&
			(mediatype == "text/html" || mediatype == "application/xhtml+xml") {
			return true
		}
	}
	return false
}
