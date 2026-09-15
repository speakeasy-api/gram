package mcp

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcprequests"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcp/sessionclientinfo"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

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
	name = mcprequests.SanitizeClientInfoField(name)
	protocolVersion = mcpversions.Sanitize(protocolVersion)
	if payload.sessionID == "" || (name == "" && protocolVersion == "") {
		return
	}

	err := store.Store(ctx, payload.projectID, payload.toolset, payload.sessionID, sessionclientinfo.Info{
		Name:            name,
		Version:         mcprequests.SanitizeClientInfoField(version),
		ProtocolVersion: protocolVersion,
	}, time.Now().UnixMilli())
	if err != nil {
		logger.WarnContext(ctx, "failed to record mcp session client info", attr.SlogError(err))
	}
}

// resolveClientIdentity determines who is calling, and reports the protocol
// version its session handshaked with when that is the only version source.
//
// The client identity has two possible sources. Under the handshake-based
// protocol revisions it comes from the initialize handshake, which is why it
// was recorded. Under the stateless model (SEP-2575 / 2026-07-28) the client
// repeats it on every request in `_meta`, and that per-call hint wins — it is
// the fresher of the two, and a client sending it may never have handshaked at
// all. This matches the precedence `@gram-ai/functions` already applies when a
// function serves MCP itself.
//
// The second return is the protocol version stored at initialize, non-empty
// only when the identity came from the stored record. It is the last-resort
// version source for callers enriching per-request telemetry: clients on the
// hint path declare their version on the request itself (header or `_meta`),
// so those callers already have a better source.
//
// The OAuth client id comes from the verified bearer token instead, so it is
// unaffected by whatever the caller reports about itself.
func resolveClientIdentity(ctx context.Context, logger *slog.Logger, store sessionClientInfoStore, payload *mcpInputs, hint *mcprequests.SanitizedClientInfo) (toolconfig.MCPClientIdentity, string) {
	identity := toolconfig.MCPClientIdentity{Name: "", Version: "", OAuthClientID: ""}
	if clientID, ok := contextvalues.GetOAuthClientID(ctx); ok {
		identity.OAuthClientID = clientID
	}

	if hint != nil {
		if name := mcprequests.SanitizeClientInfoField(hint.Name); name != "" {
			identity.Name = name
			identity.Version = mcprequests.SanitizeClientInfoField(hint.Version)
			return identity, ""
		}
	}

	if payload.sessionID == "" {
		return identity, ""
	}

	info, err := store.Load(ctx, payload.projectID, payload.toolset, payload.sessionID, time.Now().UnixMilli())
	switch {
	case errors.Is(err, sessionclientinfo.ErrNotFound):
		// An unknown caller is ordinary: no Redis, an evicted record, or a
		// client that never reported a name.
		return identity, ""
	case err != nil:
		logger.WarnContext(ctx, "failed to load mcp session client info", attr.SlogError(err))
		return identity, ""
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
	// negotiated half is derived rather than stored: this store is only
	// written by the hosted toolset handler, whose negotiation is a pure
	// function of the requested version and the surface's supported set, so
	// re-running it here reproduces the handshake's answer. Recording it is
	// what gives pre-header clients a negotiated attribute at all, since the
	// middleware has no header to read for them. A session that handshaked
	// before a change to the supported set would be attributed the answer the
	// current set produces, which is the one inaccuracy the derivation costs.
	recordMCPProtocolVersionSpan(ctx, info.ProtocolVersion, mcpversions.Negotiate(info.ProtocolVersion, mcpversions.SupportedHostedToolset()))

	return identity, info.ProtocolVersion
}
