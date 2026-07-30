package mcp

import (
	"context"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const (
	// maxClientInfoFieldLength bounds each stored client identity field. The
	// same cap is applied to the PostHog properties recorded at initialize.
	maxClientInfoFieldLength = 100
	// maxSessionIDKeyLength bounds how much of a client-supplied
	// Mcp-Session-Id is used as cache key material, so a misbehaving client
	// cannot bloat Redis keys. Mirrors
	// tunnelsessions.MaxBackendSessionIDLength.
	maxSessionIDKeyLength = 512
	// sessionClientInfoTTL is how long a handshake identity outlives its
	// initialize. Matches the default anonymous tunnel session lifetime.
	sessionClientInfoTTL = 24 * time.Hour
)

// SessionClientInfo is the MCP client identity reported during the initialize
// handshake, cached so later requests on the same session can resolve it.
//
// Under the currently-shipped MCP protocol `clientInfo` is sent only at
// initialize, and Gram's hosted MCP path is otherwise stateless — without this
// entry the identity is gone by the time the client calls a tool.
//
// The values are untrusted and self-reported. They are for observability and
// convenience only, never authorization.
type SessionClientInfo struct {
	// ProjectID and ToolsetSlug scope the entry. Session ids arrive on a
	// client-supplied header, so scoping the key keeps one tenant's sessions
	// from addressing another's.
	ProjectID   uuid.UUID `json:"-"`
	ToolsetSlug string    `json:"-"`
	SessionID   string    `json:"-"`

	Name    string `json:"name"`
	Version string `json:"version"`
}

var _ cache.CacheableObject[SessionClientInfo] = (*SessionClientInfo)(nil)

// SessionClientInfoCacheKey builds the cache key for one session's handshake
// identity. Exported so readers can look an entry up without materializing a
// partial SessionClientInfo.
func SessionClientInfoCacheKey(projectID uuid.UUID, toolsetSlug, sessionID string) string {
	return "mcpClientInfo:" + projectID.String() + ":" + toolsetSlug + ":" + conv.TruncateString(sessionID, maxSessionIDKeyLength)
}

// CacheKey implements cache.CacheableObject.
func (s SessionClientInfo) CacheKey() string {
	return SessionClientInfoCacheKey(s.ProjectID, s.ToolsetSlug, s.SessionID)
}

// AdditionalCacheKeys implements cache.CacheableObject. Single-key entry; no
// fan-out.
func (s SessionClientInfo) AdditionalCacheKeys() []string { return []string{} }

// TTL implements cache.CacheableObject.
func (s SessionClientInfo) TTL() time.Duration { return sessionClientInfoTTL }

// storeSessionClientInfo caches the identity a client reported at initialize.
// A client that reports no name leaves no entry, and a store failure is logged
// rather than surfaced: losing the identity must never fail the handshake.
func storeSessionClientInfo(ctx context.Context, logger *slog.Logger, clientInfoCache *cache.TypedCacheObject[SessionClientInfo], payload *mcpInputs, name, version string) {
	name = sanitizeClientInfoField(name)
	if name == "" || payload.sessionID == "" {
		return
	}

	err := clientInfoCache.Store(ctx, SessionClientInfo{
		ProjectID:   payload.projectID,
		ToolsetSlug: payload.toolset,
		SessionID:   payload.sessionID,
		Name:        name,
		Version:     sanitizeClientInfoField(version),
	})
	if err != nil {
		logger.WarnContext(ctx, "failed to cache mcp session client info", attr.SlogError(err))
	}
}

// resolveClientIdentity determines who is calling a tool.
//
// The client identity has two possible sources. Under the currently-shipped
// protocol it comes from the initialize handshake, which is why it was cached.
// Under the draft stateless model (SEP-2575) the client repeats it on every
// request in `_meta`, and that per-call hint wins — it is the fresher of the
// two, and a client sending it may never have handshaked at all. This matches
// the precedence `@gram-ai/functions` already applies when a function serves
// MCP itself.
//
// The OAuth client id comes from the verified bearer token instead, so it is
// unaffected by whatever the caller reports about itself.
func resolveClientIdentity(ctx context.Context, logger *slog.Logger, clientInfoCache *cache.TypedCacheObject[SessionClientInfo], payload *mcpInputs, hint *mcpClientInfoHint) toolconfig.MCPClientIdentity {
	identity := toolconfig.MCPClientIdentity{Name: "", Version: "", OAuthClientID: ""}
	if clientID, ok := contextvalues.GetOAuthClientID(ctx); ok {
		identity.OAuthClientID = clientID
	}

	if hint != nil {
		if name := sanitizeClientInfoField(hint.Name); name != "" {
			identity.Name = name
			identity.Version = sanitizeClientInfoField(hint.Version)
			return identity
		}
	}

	if payload.sessionID == "" {
		return identity
	}

	// A miss is an ordinary outcome — no Redis, an expired entry, a client
	// that never reported a name — so it stays at debug level.
	info, err := clientInfoCache.Get(ctx, SessionClientInfoCacheKey(payload.projectID, payload.toolset, payload.sessionID))
	if err != nil {
		logger.DebugContext(ctx, "no cached mcp session client info", attr.SlogError(err))
		return identity
	}

	identity.Name = info.Name
	identity.Version = info.Version

	return identity
}

// sanitizeClientInfoField bounds one untrusted client identity field. Invalid
// UTF-8 and control characters are dropped and the result is capped, so the
// value stays safe to store and, later, to hand to a function runner.
func sanitizeClientInfoField(value string) string {
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.ToValidUTF8(value, ""))

	return conv.TruncateString(cleaned, maxClientInfoFieldLength)
}
