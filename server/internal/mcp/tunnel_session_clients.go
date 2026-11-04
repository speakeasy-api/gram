package mcp

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
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

	// rowClientReadPause stops the connection writer reading after a failed
	// read, so a Redis outage can't hold up its inserts one row at a time.
	rowClientReadPause = 30 * time.Second
)

// rememberedClient is what one replica keeps about a session.
type rememberedClient struct {
	family  string
	name    string
	version string
}

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
	cache *expirable.LRU[tunnelClientCacheKey, rememberedClient]

	// rowReadsPausedUntil is unix milliseconds. Only reads for connection rows check it.
	rowReadsPausedUntil atomic.Int64
}

func newTunnelClientFamilies(store sessionClientInfoStore) *tunnelClientFamilies {
	return &tunnelClientFamilies{
		store:               store,
		cache:               expirable.NewLRU[tunnelClientCacheKey, rememberedClient](tunnelClientCacheSize, nil, tunnelClientCacheTTL),
		rowReadsPausedUntil: atomic.Int64{},
	}
}

// remembered returns a session's client from memory, else from Redis. A miss is
// remembered and a failed read is not.
func (c *tunnelClientFamilies) remembered(ctx context.Context, logger *slog.Logger, projectID uuid.UUID, scope, sessionID string) (rememberedClient, error) {
	key := tunnelClientCacheKey{projectID: projectID, scope: scope, session: sha256.Sum256([]byte(sessionID))}
	if client, ok := c.cache.Get(key); ok {
		return client, nil
	}

	ctx, cancel := context.WithTimeout(ctx, tunnelClientStoreTimeout)
	defer cancel()
	info, err := c.store.Load(ctx, projectID, scope, sessionID, time.Now().UnixMilli())
	switch {
	case errors.Is(err, sessionclientinfo.ErrNotFound):
		// No Redis, an evicted record, a session opened before this was
		// recorded, or a client that reported neither a name nor a protocol
		// version.
	case err != nil:
		logger.WarnContext(ctx, "failed to load mcp session client info", attr.SlogError(err))
		return rememberedClient{family: "", name: "", version: ""}, fmt.Errorf("load session client info: %w", err)
	}
	client := rememberedClient{family: reportedClientFamily(info.Name), name: info.Name, version: info.Version}
	c.cache.Add(key, client)
	return client, nil
}

// forRow reads a session's client for a connection row. It runs on the
// connection writer, which has no request to outlive.
func (c *tunnelClientFamilies) forRow(logger *slog.Logger, projectID uuid.UUID, scope, sessionID string) (name, version string) {
	if time.Now().UnixMilli() < c.rowReadsPausedUntil.Load() {
		return "", ""
	}
	client, err := c.remembered(context.Background(), logger, projectID, scope, sessionID)
	if err != nil {
		c.rowReadsPausedUntil.Store(time.Now().Add(rowClientReadPause).UnixMilli())
		return "", ""
	}
	return client.name, client.version
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
	key := tunnelClientCacheKey{projectID: s.projectID, scope: s.scope, session: sha256.Sum256([]byte(sessionID))}
	s.families.cache.Add(key, rememberedClient{family: reportedClientFamily(name), name: mcprequests.SanitizeClientInfoField(name), version: mcprequests.SanitizeClientInfoField(version)})
}

func (s *tunnelSessionClients) ClientFamily(ctx context.Context, sessionID string) string {
	client, err := s.families.remembered(ctx, s.logger, s.projectID, s.scope, sessionID)
	if err != nil {
		// Not remembered, so the next request retries.
		return ""
	}
	return client.family
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
