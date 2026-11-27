package skilltree

import (
	"mime"
	"strings"
	"unicode/utf8"
)

// textMediaTypes are the non-text/* media types whose content is text a
// reader can show as-is.
var textMediaTypes = map[string]bool{
	"application/json":       true,
	"application/javascript": true,
	"application/toml":       true,
	"application/xml":        true,
	"application/yaml":       true,
	"application/x-sh":       true,
	"application/x-yaml":     true,
	"image/svg+xml":          true,
}

// IsText reports whether a supporting file can be shown as UTF-8 text: its
// media type is a text type and its content is valid UTF-8. Surfaces that
// return file content use it to choose between text and a binary encoding.
func IsText(contentType string, content []byte) bool {
	return isTextMediaType(contentType) && utf8.Valid(content)
}

// isTextMediaType reports whether a content type names text: text/*, a known
// text-based application type, or a +json or +xml structured-syntax suffix.
// A malformed content type is judged by its media type with any parameters
// removed.
func isTextMediaType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType, _, _ = strings.Cut(contentType, ";")
		mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	}
	return strings.HasPrefix(mediaType, "text/") ||
		textMediaTypes[mediaType] ||
		strings.HasSuffix(mediaType, "+json") ||
		strings.HasSuffix(mediaType, "+xml")
}
