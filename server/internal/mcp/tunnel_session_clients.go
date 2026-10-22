package mcp

import (
	"context"
	"crypto/sha256"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcprequests"
	"github.com/speakeasy-api/gram/server/internal/mcp/sessionclientinfo"
	"github.com/speakeasy-api/gram/tunnel/metrics"
)

const (
	// tunnelClientInfoScopePrefix starts the client-info scope of a tunneled
	// MCP server. The ":" cannot collide with a toolset slug.
	tunnelClientInfoScopePrefix = "tunnel:"

	// tunnelClientCacheTTL bounds how long a replica trusts its memory of a
	// session's family. It also paces the Redis reads that keep an active
	// session's record from being evicted as dormant.
	tunnelClientCacheTTL = 10 * time.Minute

	// tunnelClientCacheSize caps the sessions one replica remembers.
	tunnelClientCacheSize = 16384

	// tunnelClientStoreTimeout bounds each Redis call. Attribution is best
	// effort and must never hold up an MCP request.
	tunnelClientStoreTimeout = 250 * time.Millisecond
)

type tunnelClientCacheKey struct {
	projectID uuid.UUID
	scope     string
	// session is a digest so raw session ids are not held in memory.
	session [sha256.Size]byte
}

// tunnelClientFamilies attributes authenticated tunneled traffic to the client
// that opened each session. Speakeasy relays these sessions without
// terminating them, so the record is keyed by the MCP server's own session id
// and kept in the session client-info store the hosted path uses. Requests
// land on any replica, so Redis is the shared record; an in-process cache in
// front of it means a session costs one Redis read per replica per TTL rather
// than one per request. Misses are cached too: the record is written before
// the initialize response reaches the client, so a session's first follow-up
// cannot outrun it.
type tunnelClientFamilies struct {
	store sessionClientInfoStore
	cache *expirable.LRU[tunnelClientCacheKey, string]
}

func newTunnelClientFamilies(store sessionClientInfoStore) *tunnelClientFamilies {
	return &tunnelClientFamilies{
		store: store,
		cache: expirable.NewLRU[tunnelClientCacheKey, string](tunnelClientCacheSize, nil, tunnelClientCacheTTL),
	}
}

// forServer scopes session records to one tunneled MCP server, since session
// ids arrive on a client-supplied header.
func (c *tunnelClientFamilies) forServer(logger *slog.Logger, projectID, mcpServerID uuid.UUID) *tunnelSessionClients {
	return &tunnelSessionClients{
		families:  c,
		logger:    logger,
		projectID: projectID,
		scope:     tunnelClientInfoScopePrefix + mcpServerID.String(),
	}
}

// tunnelSessionClients implements proxy.SessionClients for one tunneled MCP
// server.
type tunnelSessionClients struct {
	families  *tunnelClientFamilies
	logger    *slog.Logger
	projectID uuid.UUID
	scope     string
}

func (s *tunnelSessionClients) key(sessionID string) tunnelClientCacheKey {
	return tunnelClientCacheKey{projectID: s.projectID, scope: s.scope, session: sha256.Sum256([]byte(sessionID))}
}

func (s *tunnelSessionClients) RecordInitialize(ctx context.Context, sessionID string, params *mcp.InitializeParams) {
	if params == nil {
		return
	}
	name, version := "", ""
	if params.ClientInfo != nil {
		name, version = params.ClientInfo.Name, params.ClientInfo.Version
	}

	ctx, cancel := context.WithTimeout(ctx, tunnelClientStoreTimeout)
	defer cancel()
	storeSessionClientInfo(ctx, s.logger, s.families.store, &mcpInputs{ //nolint:exhaustruct // only the record's identity fields matter here
		projectID:       s.projectID,
		sessionID:       sessionID,
		clientInfoScope: s.scope,
	}, name, version, params.ProtocolVersion)
	s.families.cache.Add(s.key(sessionID), reportedClientFamily(name))
}

func (s *tunnelSessionClients) ClientFamily(ctx context.Context, sessionID string) string {
	key := s.key(sessionID)
	if family, ok := s.families.cache.Get(key); ok {
		return family
	}

	ctx, cancel := context.WithTimeout(ctx, tunnelClientStoreTimeout)
	defer cancel()
	info, err := s.families.store.Load(ctx, s.projectID, s.scope, sessionID, time.Now().UnixMilli())
	switch {
	case errors.Is(err, sessionclientinfo.ErrNotFound):
		// No Redis, an evicted record, a session opened before this was
		// recorded, or a client that reported neither a name nor a protocol
		// version.
	case err != nil:
		// Not cached, so the next request retries.
		s.logger.WarnContext(ctx, "failed to load tunneled mcp session client info", attr.SlogError(err))
		return ""
	}
	family := reportedClientFamily(info.Name)
	s.families.cache.Add(key, family)
	return family
}

// reportedClientFamily classifies a reported clientInfo.name, or returns ""
// when the client reported none.
func reportedClientFamily(name string) string {
	name = mcprequests.SanitizeClientInfoField(name)
	if name == "" {
		return ""
	}
	return metrics.ClientFamily(name)
}

// recordedClientFamily serves a family already resolved with the session's
// routing record, so attribution costs no further lookup.
type recordedClientFamily string

func (recordedClientFamily) RecordInitialize(context.Context, string, *mcp.InitializeParams) {}

func (f recordedClientFamily) ClientFamily(context.Context, string) string {
	return string(f)
}
