package hooks

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	redisCache "github.com/go-redis/cache/v9"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// deviceMCPListTTL is how long a device's copy of its last complete MCP
// inventory survives. A session's own inventory is empty for the first
// seconds while the relay lists the configured servers; a day covers the next
// session of a normal working day on the same device, and an older copy is
// too likely to miss servers the user has since installed or removed.
const deviceMCPListTTL = 24 * time.Hour

// deviceMCPListIdentityKind says which device identity a device MCP inventory
// copy is stored under.
type deviceMCPListIdentityKind string

const (
	// deviceMCPListIdentityDevice keys a copy by the per-device id (Claude
	// user.id), learned from the session's OTEL stream.
	deviceMCPListIdentityDevice deviceMCPListIdentityKind = "device"

	// deviceMCPListIdentityHost keys a copy by the hostname the hook sender
	// reports on every event. A new session's first hook arrives before its
	// OTEL stream, so the device id is often not known yet when the copy is
	// needed.
	deviceMCPListIdentityHost deviceMCPListIdentityKind = "host"
)

// deviceMCPListScope identifies whose and which device MCP inventory copy a
// read or write addresses. Every field is part of the cache key, so a copy is
// only ever read back by the same user, on the same device, through the same
// hook adapter, in the same working directory.
type deviceMCPListScope struct {
	// ProjectID is the Speakeasy project the inventory was reported to.
	ProjectID string

	// UserID is the Speakeasy user the session is attributed to.
	UserID string

	// DeviceID is the per-device id, empty when not known yet.
	DeviceID string

	// Hostname is the hook sender's reported hostname, empty when not known.
	Hostname string

	// Adapter is the hook adapter or source that reported the inventory.
	Adapter string

	// Cwd is the session's working directory, which selects project-scoped
	// MCP configuration.
	Cwd string
}

// deviceMCPList is one stored device MCP inventory copy.
type deviceMCPList struct {
	// Owner is the authenticated actor that wrote the copy.
	Owner mcpListOwner `json:"owner"`

	// UserID is the Speakeasy user the copy belongs to.
	UserID string `json:"user_id"`

	// Entries is the complete MCP inventory last reported for the scope.
	Entries []MCPServerEntry `json:"entries"`
}

// identities lists the device identities the scope can be keyed by, the
// device id first because it is the stronger identity.
func (scope deviceMCPListScope) identities() []deviceMCPListIdentity {
	var identities []deviceMCPListIdentity
	if deviceID := strings.TrimSpace(scope.DeviceID); deviceID != "" {
		identities = append(identities, deviceMCPListIdentity{kind: deviceMCPListIdentityDevice, value: deviceID})
	}
	if hostname := strings.TrimSpace(scope.Hostname); hostname != "" {
		identities = append(identities, deviceMCPListIdentity{kind: deviceMCPListIdentityHost, value: hostname})
	}
	return identities
}

func (scope deviceMCPListScope) usable() bool {
	return strings.TrimSpace(scope.ProjectID) != "" &&
		strings.TrimSpace(scope.UserID) != "" &&
		strings.TrimSpace(scope.Adapter) != "" &&
		len(scope.identities()) > 0
}

type deviceMCPListIdentity struct {
	kind  deviceMCPListIdentityKind
	value string
}

// deviceMCPListCacheKey returns the Redis key of a device MCP inventory copy.
// The user, device identity, adapter and working directory are hashed so a
// client-reported hostname or path cannot change the key's shape.
func deviceMCPListCacheKey(scope deviceMCPListScope, identity deviceMCPListIdentity) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{
		strings.TrimSpace(scope.UserID),
		string(identity.kind),
		identity.value,
		strings.ToLower(strings.TrimSpace(scope.Adapter)),
		strings.TrimSpace(scope.Cwd),
	}, "\x00")))
	return fmt.Sprintf("device:mcp-list:v1:%s:%x", strings.TrimSpace(scope.ProjectID), digest)
}

// cacheDeviceMCPList stores a complete MCP inventory as the device copy for
// scope, under every device identity the scope has. Only a human sender
// authenticated to the scope's project writes a copy: agents have no device
// identity, and an unauthenticated hook proves only a session id. Best-effort.
func (s *Service) cacheDeviceMCPList(ctx context.Context, scope deviceMCPListScope, entries []MCPServerEntry) {
	if !scope.usable() || isAgentActor(ctx) {
		return
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil || authCtx.ProjectID.String() != scope.ProjectID {
		return
	}
	if entries == nil {
		entries = []MCPServerEntry{}
	}

	value := deviceMCPList{
		Owner:   mcpListOwnerFromContext(ctx),
		UserID:  strings.TrimSpace(scope.UserID),
		Entries: entries,
	}
	for _, identity := range scope.identities() {
		if err := s.cache.Set(ctx, deviceMCPListCacheKey(scope, identity), value, deviceMCPListTTL); err != nil {
			s.logger.WarnContext(ctx, "failed to cache device MCP list",
				attr.SlogEvent("hook_device_mcp_list_cache_set_failed"),
				attr.SlogError(err),
			)
		}
	}
}

// getDeviceMCPList returns the device copy of the last complete MCP inventory
// for scope, and false when there is none. It applies the session snapshot's
// owner rule and never returns a copy written for another user or to an
// agent.
func (s *Service) getDeviceMCPList(ctx context.Context, scope deviceMCPListScope) ([]MCPServerEntry, bool) {
	if !scope.usable() || isAgentActor(ctx) {
		return nil, false
	}
	reader := mcpListOwnerFromContext(ctx)
	for _, identity := range scope.identities() {
		var stored deviceMCPList
		err := s.cache.Get(ctx, deviceMCPListCacheKey(scope, identity), &stored)
		if err != nil {
			if !errors.Is(err, redisCache.ErrCacheMiss) {
				s.logger.WarnContext(ctx, "failed to read device MCP list",
					attr.SlogEvent("hook_device_mcp_list_cache_get_failed"),
					attr.SlogError(err),
				)
			}
			continue
		}
		if stored.UserID != strings.TrimSpace(scope.UserID) || !stored.Owner.shares(reader) {
			continue
		}
		return stored.Entries, true
	}
	return nil, false
}
