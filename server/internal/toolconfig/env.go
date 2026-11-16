// Package toolconfig provides configuration types for tool execution.
package toolconfig

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
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

	// LoadSystemEnv loads and merges source, toolset, and attached environments.
	// Merges in order: source env (base) -> toolset env -> attached env (highest priority).
	// Returns empty map if no environments exist.
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

// Has reports whether a key is declared (case-insensitive), regardless of value.
func (c *CaseInsensitiveEnv) Has(key string) bool {
	_, ok := c.data[strings.ToLower(key)]
	return ok
}

// Set stores a value by key (case-insensitive).
func (c *CaseInsensitiveEnv) Set(key, value string) {
	c.data[strings.ToLower(key)] = value
}

// All returns a copy of all stored key-value pairs.
func (c *CaseInsensitiveEnv) All() map[string]string {
	result := make(map[string]string, len(c.data))
	maps.Copy(result, c.data)
	return result
}

// Delete removes a key from the environment (case-insensitive).
func (c *CaseInsensitiveEnv) Delete(key string) {
	delete(c.data, strings.ToLower(key))
}

// Keys returns all keys in the environment.
func (c *CaseInsensitiveEnv) Keys() []string {
	keys := make([]string, 0, len(c.data))
	for k := range c.data {
		keys = append(keys, k)
	}
	return keys
}

// MCPClientIdentity describes who is on the other end of an MCP tool call.
//
// Name and Version are self-reported by the client. OAuthClientID is read from
// the caller's verified bearer token, so it is the sounder of the two here —
// but it is forwarded to tools as plain request metadata, where that
// provenance no longer travels with it. Both are for attribution and
// observability; authorization stays on this side of the boundary, where the
// credential is actually checked.
//
// Every field is optional: direct (non-MCP) tool calls populate none of them,
// and an MCP client may report an identity without authenticating, or the
// reverse.
type MCPClientIdentity struct {
	Name          string
	Version       string
	OAuthClientID string
}

// IsZero reports whether nothing is known about the caller.
func (m MCPClientIdentity) IsZero() bool {
	return m.Name == "" && m.Version == "" && m.OAuthClientID == ""
}

// ToolCallEnv holds the environment configuration for a tool call.
// SystemEnv contains base values, UserConfig contains user overrides.
type ToolCallEnv struct {
	SystemEnv  *CaseInsensitiveEnv
	UserConfig *CaseInsensitiveEnv
	GramEmail  string // Authenticated Speakeasy user's email (empty if not applicable)
	// GramChatID is the authoritative assistant chat UUID when supplied by the caller.
	GramChatID string
	// MCPClient identifies the MCP caller, when the tool was called over MCP.
	MCPClient MCPClientIdentity

	// Credentials, when set, is filled with the credentials an HTTP tool call sent upstream.
	Credentials *CredentialReport
}

// CredentialReport is what an HTTP tool call sent upstream to authenticate.
type CredentialReport struct {
	// Sent lists each credential the request carried, including a client secret whose grant failed.
	Sent []AppliedCredential

	// GrantFailure says why a client credentials grant failed. Empty when none did.
	GrantFailure ClientGrantFailure

	// CallerVariables lists the variables the tool call's own arguments set; a credential from one is the caller's.
	CallerVariables []string

	// ServerVariable is the user configuration variable the server URL came from, or "" for the tool's own URL or a system one.
	ServerVariable string

	// Challenges are the provider response's WWW-Authenticate values.
	Challenges []string
}

// AppliedCredential is one credential an HTTP tool call used.
type AppliedCredential struct {
	// Variable is the environment variable the value came from.
	Variable string

	// System reports that the value came from SystemEnv rather than UserConfig.
	System bool

	// ClientGrant reports a client secret exchanged for a token, not sent as is.
	ClientGrant bool

	// Carrier is the request header that carried it, or "?" and the name of a query parameter.
	Carrier string
}

// KeepCarried drops the credentials the final request no longer carried, as after a redirect
// to another origin strips them.
func (r *CredentialReport) KeepCarried(final *http.Request) {
	if r == nil || final == nil {
		return
	}
	r.Sent = slices.DeleteFunc(r.Sent, func(credential AppliedCredential) bool {
		// A secret whose grant failed was never sent; it stands for the failed grant.
		if credential.ClientGrant && r.GrantFailure != "" {
			return false
		}
		if name, ok := strings.CutPrefix(credential.Carrier, "?"); ok {
			return final.URL == nil || !final.URL.Query().Has(name)
		}
		return final.Header.Get(credential.Carrier) == ""
	})
}

// ClientGrantFailure is why a client credentials token request failed.
type ClientGrantFailure string

const (
	// ClientGrantRejected means the token endpoint refused the client (400, 401 or 403).
	ClientGrantRejected ClientGrantFailure = "rejected"

	// ClientGrantUnavailable means the token endpoint was unreachable, timed out, or answered 429 or 5xx.
	ClientGrantUnavailable ClientGrantFailure = "unavailable"

	// ClientGrantBlocked means the token URL resolves to an address Speakeasy may not call.
	ClientGrantBlocked ClientGrantFailure = "blocked"

	// ClientGrantMisconfigured means the token endpoint gave any other answer.
	ClientGrantMisconfigured ClientGrantFailure = "misconfigured"
)

// Merged returns a case-insensitive view where UserConfig values override
// SystemEnv values. Handles nil SystemEnv / UserConfig. Never returns nil.
func (e ToolCallEnv) Merged() *CaseInsensitiveEnv {
	merged := NewCaseInsensitiveEnv()
	if e.SystemEnv != nil {
		for k, v := range e.SystemEnv.All() {
			merged.Set(k, v)
		}
	}
	if e.UserConfig != nil {
		for k, v := range e.UserConfig.All() {
			merged.Set(k, v)
		}
	}
	return merged
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
