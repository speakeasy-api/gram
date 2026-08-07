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
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
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

// storeSessionClientInfo records what a client reported about itself at
// initialize. A client that reports neither a name nor a protocol version
// leaves no record, and a write failure is logged rather than surfaced: losing
// this must never fail the handshake.
//
// Either field alone is enough to be worth recording. A client that omits
// clientInfo.name but sends protocolVersion is still attributable to a protocol
// generation for the rest of its session, which is the more useful of the two
// for diagnosing version-specific behaviour. Admitting those records does not
// affect how much can be stored: the per-server record cap is what bounds that.
func storeSessionClientInfo(ctx context.Context, logger *slog.Logger, store sessionClientInfoStore, payload *mcpInputs, name, version, protocolVersion string) {
	name = sanitizeClientInfoField(name)
	protocolVersion = mcpversions.Sanitize(protocolVersion)
	if payload.sessionID == "" || (name == "" && protocolVersion == "") {
		return
	}

	err := store.Store(ctx, payload.projectID, payload.toolset, payload.sessionID, sessionclientinfo.Info{
		Name:            name,
		Version:         sanitizeClientInfoField(version),
		ProtocolVersion: protocolVersion,
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

	// Attribute this request to the protocol generation its session handshaked
	// with. Piggybacking on the Load above is deliberate: it makes propagation
	// free on the one non-initialize request that already reads the store,
	// rather than adding a Redis round-trip to every RPC. Clients that send the
	// MCP-Protocol-Version header are already covered on every request by
	// middleware.MCPProtocolVersionTelemetry; this fills the gap for clients
	// predating that header, which is why it is best-effort by design.
	//
	// The stored value is what the client asked for at initialize. The
	// negotiated half is deterministic rather than stored: this store is only
	// written by the hosted toolset handler, which answers
	// ServedHostedToolset unconditionally. Recording it here is what gives
	// pre-header clients a negotiated attribute at all, since the middleware
	// has no header to read for them. A session that handshaked before a
	// change to that constant would be attributed the current value, which is
	// the one inaccuracy the determinism costs.
	recordMCPProtocolVersionSpan(ctx, info.ProtocolVersion, mcpversions.ServedHostedToolset)

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
