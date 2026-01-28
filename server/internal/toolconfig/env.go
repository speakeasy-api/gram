// Package toolconfig provides configuration types for tool execution.
package toolconfig

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ettle/strcase"
	"github.com/google/uuid"
)

// ErrNotFound is returned when an environment does not exist.
var ErrNotFound = errors.New("environment not found")

// EnvironmentLoader loads environment variables for tool execution.
type EnvironmentLoader interface {
	// Load retrieves the environment variables for a given project and environment slug or ID.
	//
	// # Errors
	//   * [ErrNotFound]: when the environment does not exist.
	//   * `error`: when an unrecognized error occurs.
	Load(ctx context.Context, projectID uuid.UUID, environmentID SlugOrID) (map[string]string, error)

	// LoadSystemEnv loads and merges source and toolset environments.
	// Merges in order: source env (base) -> toolset env (override).
	// Returns empty map if neither environment exists.
	//
	// # Errors
	//   * `error`: when an unrecognized error occurs.
	LoadSystemEnv(ctx context.Context, projectID uuid.UUID, toolsetID uuid.UUID, sourceKind string, sourceSlug string) (*CaseInsensitiveEnv, error)
}

// SlugOrID represents either a slug string or a UUID identifier.
type SlugOrID struct {
	ID   uuid.UUID
	Slug string
}

// NilSlugOrID is an empty SlugOrID.
var NilSlugOrID = SlugOrID{
	ID:   uuid.Nil,
	Slug: "",
}

// ID creates a SlugOrID from a UUID.
func ID(id uuid.UUID) SlugOrID {
	return SlugOrID{
		ID:   id,
		Slug: "",
	}
}

// Slug creates a SlugOrID from a slug string.
func Slug(slug string) SlugOrID {
	return SlugOrID{
		Slug: slug,
		ID:   uuid.Nil,
	}
}

func (s *SlugOrID) String() string {
	if s.ID != uuid.Nil {
		return fmt.Sprintf("UUID(%s)", s.ID)
	}

	return fmt.Sprintf("Slug(%q)", s.Slug)
}

// IsEmpty returns true if both ID and Slug are empty.
func (s *SlugOrID) IsEmpty() bool {
	return s.ID == uuid.Nil && s.Slug == ""
}

// CaseInsensitiveEnv stores environment variables with case-insensitive key lookup.
type CaseInsensitiveEnv struct {
	data map[string]string
}

// NewCaseInsensitiveEnv creates a new empty CaseInsensitiveEnv.
func NewCaseInsensitiveEnv() *CaseInsensitiveEnv {
	return &CaseInsensitiveEnv{data: make(map[string]string)}
}

// CIEnvFrom creates a CaseInsensitiveEnv from a map of variables.
func CIEnvFrom(vars map[string]string) *CaseInsensitiveEnv {
	env := NewCaseInsensitiveEnv()
	for k, v := range vars {
		env.Set(k, v)
	}
	return env
}

// Get retrieves a value by key (case-insensitive).
func (c *CaseInsensitiveEnv) Get(key string) string {
	return c.data[strings.ToLower(key)]
}

// Set stores a value by key (case-insensitive).
func (c *CaseInsensitiveEnv) Set(key, value string) {
	c.data[strings.ToLower(key)] = value
}

// All returns a copy of all stored key-value pairs.
func (c *CaseInsensitiveEnv) All() map[string]string {
	result := make(map[string]string, len(c.data))
	for k, v := range c.data {
		result[k] = v
	}
	return result
}

// ToolCallEnv holds the environment configuration for a tool call.
// SystemEnv contains base values, UserConfig contains user overrides.
// OAuthToken is an optional OAuth token for external MCP servers that require authentication.
type ToolCallEnv struct {
	SystemEnv  *CaseInsensitiveEnv
	UserConfig *CaseInsensitiveEnv
	OAuthToken string // OAuth token for external MCP servers (empty if not applicable)
}

// ToNormalizedEnvKey converts a string to a normalized environment variable key
// (lowercase snake_case).
func ToNormalizedEnvKey(s string) string {
	return strings.ToLower(strcase.ToSNAKE(s))
}

// ToPosixName converts a string to POSIX-compliant SCREAMING_SNAKE_CASE.
func ToPosixName(s string) string {
	return strcase.ToSNAKE(s)
}

// ToHTTPHeader converts a string to HTTP header format (Title-Case).
func ToHTTPHeader(s string) string {
	return strcase.ToCase(s, strcase.TitleCase, '-')
}
