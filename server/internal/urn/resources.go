package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/constants"
)

type Resource struct {
	Kind         ResourceKind
	Source       string
	SlugifiedURI string

	checked bool
	err     error
}

var (
	nonSlugCharsRE    = regexp.MustCompile(`[^a-z0-9_-]+`)
	multiDashRE       = regexp.MustCompile(`-+`)
	multiUnderscoreRE = regexp.MustCompile(`_+`)
)

func santitizeToURIFragment(s string) string {
	if s == "" {
		return ""
	}

	// Convert to lowercase
	s = strings.ToLower(s)

	// Replace non-slug characters with dash
	s = nonSlugCharsRE.ReplaceAllString(s, "-")

	// Clean up multiple consecutive dashes
	s = multiDashRE.ReplaceAllString(s, "-")

	// Clean up multiple consecutive underscores
	s = multiUnderscoreRE.ReplaceAllString(s, "_")

	// Trim leading and trailing dashes/underscores
	s = strings.Trim(s, "-_")

	// Trim to 128 characters to comply with maxSegmentLength
	if len(s) > maxSegmentLength {
		s = s[:maxSegmentLength]
		// Re-trim in case we cut in the middle of trailing dashes
		s = strings.Trim(s, "-_")
	}

	return s
}

// uriToSlug converts a URI into a slug-friendly format suitable for URN usage.
// It includes the scheme, host, path, and query parameters to ensure uniqueness across different URI types.
// Examples:
//
//	file:///project/src/main.rs -> file-project-src-main-rs
//	postgres://database/customers/schema -> postgres-database-customers-schema
//	screen://localhost/display1 -> screen-localhost-display1
//	https://api.example.com/data?version=v1&format=json -> https-api-example-com-data-version-v1-format-json
func uriToSlug(uri string) string {
	// Empty URI is invalid - return empty string to fail validation
	if uri == "" {
		return ""
	}

	// Parse the URI to extract components
	parsed, err := url.Parse(uri)
	if err != nil {
		// If parsing fails, sanitize the raw URI string
		sanitized := santitizeToURIFragment(uri)
		return sanitized
	}

	// Build slug from URI components
	var parts []string

	// Add scheme if present (e.g., "file", "postgres", "screen")
	// This ensures different schemes produce unique URNs
	if parsed.Scheme != "" {
		parts = append(parts, santitizeToURIFragment(parsed.Scheme))
	}

	// Add host if present (e.g., "localhost", "database")
	if parsed.Host != "" {
		parts = append(parts, santitizeToURIFragment(parsed.Host))
	}

	// Add path if present (e.g., "/api/users" becomes "api-users")
	if parsed.Path != "" && parsed.Path != "/" {
		pathPart := strings.Trim(parsed.Path, "/")
		pathPart = strings.ReplaceAll(pathPart, "/", "-")
		parts = append(parts, santitizeToURIFragment(pathPart))
	}

	// Add query parameters if present (e.g., "?version=v1&format=json" becomes "version-v1-format-json")
	if parsed.RawQuery != "" {
		// Convert query string to a slug-friendly format
		queryPart := strings.ReplaceAll(parsed.RawQuery, "&", "-")
		queryPart = strings.ReplaceAll(queryPart, "=", "-")
		parts = append(parts, santitizeToURIFragment(queryPart))
	}

	// Filter out empty parts and join with dash
	var filtered []string
	for _, p := range parts {
		if p != "" {
			filtered = append(filtered, p)
		}
	}

	// If no valid parts found, return empty string to fail validation
	if len(filtered) == 0 {
		return ""
	}

	result := strings.Join(filtered, "-")

	// Ensure the final result doesn't exceed maxSegmentLength
	if len(result) > maxSegmentLength {
		result = result[:maxSegmentLength]
		// Re-trim in case we cut in the middle of trailing dashes
		result = strings.Trim(result, "-_")
	}

	return result
}

func NewResource(kind ResourceKind, source, uri string) Resource {
	r := Resource{
		Kind:         kind,
		Source:       source,
		SlugifiedURI: uriToSlug(uri),

		checked: false,
		err:     nil,
	}

	_ = r.validate()

	return r
}

func ParseResource(value string) (Resource, error) {
	if value == "" {
		return Resource{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 4)
	if len(parts) != 4 {
		return Resource{}, fmt.Errorf("%w: expected four segments", ErrInvalid)
	}

	if parts[0] != "resources" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return Resource{}, fmt.Errorf("%w: expected resources urn (got: %q)", ErrInvalid, truncated)
	}

	r := Resource{
		Kind:         ResourceKind(parts[1]),
		Source:       parts[2],
		SlugifiedURI: parts[3],

		checked: false,
		err:     nil,
	}

	if err := r.validate(); err != nil {
		return Resource{}, err
	}

	return r, nil
}

func (u Resource) IsZero() bool {
	return u.Kind == "" && u.Source == "" && u.SlugifiedURI == ""
}

func (u Resource) String() string {
	return "resources" + delimiter + string(u.Kind) + delimiter + u.Source + delimiter + u.SlugifiedURI
}

func (u Resource) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("resource urn to json: %w", err)
	}

	return b, nil
}

func (u *Resource) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read resource urn string from json: %w", err)
	}

	parsed, err := ParseResource(s)
	if err != nil {
		return fmt.Errorf("parse resource urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *Resource) Scan(value any) error {
	if value == nil {
		return nil
	}

	var s string
	switch v := value.(type) {
	case string:
		s = v
	case []byte:
		s = string(v)
	default:
		return fmt.Errorf("cannot scan %T into Resource", value)
	}

	parsed, err := ParseResource(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u Resource) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u Resource) MarshalText() (text []byte, err error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal resource urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *Resource) UnmarshalText(text []byte) error {
	parsed, err := ParseResource(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal resource urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *Resource) validate() error {
	if u.checked {
		return u.err
	}

	u.checked = true

	parts := [][2]string{
		{"kind", string(u.Kind)},
		{"source", u.Source},
		{"slugified_uri", u.SlugifiedURI},
	}

	for _, part := range parts {
		segment, v := part[0], part[1]
		if v == "" {
			u.err = fmt.Errorf("%w: empty %s", ErrInvalid, segment)
			return u.err
		}

		if len(v) > maxSegmentLength {
			u.err = fmt.Errorf("%w: %s segment is too long (max %d, got %d)", ErrInvalid, segment, maxSegmentLength, len(v))
			return u.err
		}

		if !constants.SlugPatternRE.MatchString(v) {
			u.err = fmt.Errorf("%w: disallowed characters in %s: %q", ErrInvalid, segment, v)
			return u.err
		}
	}

	if _, ok := resourceKinds[u.Kind]; !ok {
		u.err = fmt.Errorf("%w: unknown resource kind: %q", ErrInvalid, u.Kind)
		return u.err
	}

	return nil
}
