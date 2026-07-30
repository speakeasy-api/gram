package mcp

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/sessionclientinfo"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

// maxClientInfoFieldLength bounds each stored client identity field. The same
// cap is applied to the PostHog properties recorded at initialize.
const maxClientInfoFieldLength = 100

// sessionClientInfoStore records and resolves the identity a client reported
// during the initialize handshake. Narrow interface so resolution can be
// exercised without Redis; the production implementation is
// sessionclientinfo.Store.
type sessionClientInfoStore interface {
	Store(ctx context.Context, projectID uuid.UUID, toolsetSlug, sessionID string, info sessionclientinfo.Info, nowMillis int64) error
	Load(ctx context.Context, projectID uuid.UUID, toolsetSlug, sessionID string, nowMillis int64) (sessionclientinfo.Info, error)
}

// storeSessionClientInfo records the identity a client reported at initialize.
// A client that reports no name leaves no record, and a write failure is
// logged rather than surfaced: losing the identity must never fail the
// handshake.
func storeSessionClientInfo(ctx context.Context, logger *slog.Logger, store sessionClientInfoStore, payload *mcpInputs, name, version string) {
	name = sanitizeClientInfoField(name)
	if name == "" || payload.sessionID == "" {
		return
	}

	err := store.Store(ctx, payload.projectID, payload.toolset, payload.sessionID, sessionclientinfo.Info{
		Name:    name,
		Version: sanitizeClientInfoField(version),
	}, time.Now().UnixMilli())
	if err != nil {
		logger.WarnContext(ctx, "failed to record mcp session client info", attr.SlogError(err))
	}
}

// resolveClientIdentity determines who is calling a tool.
//
// The client identity has two possible sources. Under the currently-shipped
// protocol it comes from the initialize handshake, which is why it was
// recorded. Under the draft stateless model (SEP-2575) the client repeats it
// on every request in `_meta`, and that per-call hint wins — it is the fresher
// of the two, and a client sending it may never have handshaked at all. This
// matches the precedence `@gram-ai/functions` already applies when a function
// serves MCP itself.
//
// The OAuth client id comes from the verified bearer token instead, so it is
// unaffected by whatever the caller reports about itself.
func resolveClientIdentity(ctx context.Context, logger *slog.Logger, store sessionClientInfoStore, payload *mcpInputs, hint *mcpClientInfoHint) toolconfig.MCPClientIdentity {
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

	info, err := store.Load(ctx, payload.projectID, payload.toolset, payload.sessionID, time.Now().UnixMilli())
	switch {
	case errors.Is(err, sessionclientinfo.ErrNotFound):
		// An unknown caller is ordinary: no Redis, an evicted record, or a
		// client that never reported a name.
		return identity
	case err != nil:
		logger.WarnContext(ctx, "failed to load mcp session client info", attr.SlogError(err))
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
