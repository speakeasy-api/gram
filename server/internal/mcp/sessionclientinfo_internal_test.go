package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

// mapCache is a minimal in-process cache.Cache for exercising resolution
// precedence without a Redis round trip. Values round-trip through JSON so the
// stored shape matches production.
type mapCache struct {
	entries map[string][]byte
}

var _ cache.Cache = (*mapCache)(nil)

func newMapCache() *mapCache { return &mapCache{entries: map[string][]byte{}} }

func (c *mapCache) Get(_ context.Context, key string, value any) error {
	raw, ok := c.entries[key]
	if !ok {
		return errNoTestCacheEntry
	}
	if err := json.Unmarshal(raw, value); err != nil {
		return errNoTestCacheEntry
	}
	return nil
}

func (c *mapCache) Set(_ context.Context, key string, value any, _ time.Duration) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err //nolint:wrapcheck // test double
	}
	c.entries[key] = raw
	return nil
}

func (c *mapCache) GetAndDelete(ctx context.Context, key string, value any) error {
	if err := c.Get(ctx, key, value); err != nil {
		return err
	}
	delete(c.entries, key)
	return nil
}

func (c *mapCache) Add(_ context.Context, key string, _ time.Duration) (bool, error) {
	if _, ok := c.entries[key]; ok {
		return false, nil
	}
	c.entries[key] = []byte("{}")
	return true, nil
}

func (c *mapCache) Update(ctx context.Context, key string, value any) error {
	return c.Set(ctx, key, value, 0)
}

func (c *mapCache) Delete(_ context.Context, key string) error {
	delete(c.entries, key)
	return nil
}

func (c *mapCache) Expire(context.Context, string, time.Duration) error { return nil }

func (c *mapCache) ListAppend(context.Context, string, any, time.Duration) error { return nil }

func (c *mapCache) ListRange(context.Context, string, int64, int64, any) error { return nil }

func (c *mapCache) DeleteByPrefix(context.Context, string) error { return nil }

var errNoTestCacheEntry = errors.New("no cache entry for key")

func newClientIdentityFixture(t *testing.T) (*cache.TypedCacheObject[SessionClientInfo], *mcpInputs) {
	t.Helper()

	backing := newMapCache()
	typed := cache.NewTypedObjectCache[SessionClientInfo](testenv.NewLogger(t), backing, cache.SuffixNone)

	return &typed, &mcpInputs{
		projectID:             uuid.New(),
		toolset:               "widgets",
		environment:           "",
		mcpEnvVariables:       nil,
		oauthTokenInputs:      nil,
		authenticated:         false,
		sessionID:             "session-1",
		chatID:                "",
		mode:                  ToolModeStatic,
		userID:                "",
		externalUserID:        "",
		apiKeyID:              "",
		toolVariationsGroupID: nil,
		mcpServerID:           nil,
		tags:                  nil,
	}
}

// TestSessionClientInfoCacheKey_DistinguishesLongSessionIDs guards the reason
// the session id is hashed: a prefix-truncated key would map every id sharing
// that prefix onto one entry, handing one session another's cached identity.
func TestSessionClientInfoCacheKey_DistinguishesLongSessionIDs(t *testing.T) {
	t.Parallel()

	projectID := uuid.New()
	shared := strings.Repeat("s", 4096)

	first := SessionClientInfoCacheKey(projectID, "widgets", shared+"a")
	second := SessionClientInfoCacheKey(projectID, "widgets", shared+"b")

	require.NotEqual(t, first, second)
	require.Len(t, second, len(first), "keys are fixed length regardless of session id size")
	require.NotContains(t, first, shared, "the raw session id must not land in the keyspace")
}

func TestSessionClientInfoCacheKey_ScopedPerProjectAndToolset(t *testing.T) {
	t.Parallel()

	sessionID := "session-1"
	projectA, projectB := uuid.New(), uuid.New()

	require.NotEqual(t,
		SessionClientInfoCacheKey(projectA, "widgets", sessionID),
		SessionClientInfoCacheKey(projectB, "widgets", sessionID),
	)
	require.NotEqual(t,
		SessionClientInfoCacheKey(projectA, "widgets", sessionID),
		SessionClientInfoCacheKey(projectA, "gadgets", sessionID),
	)
}

func TestResolveClientIdentity_FallsBackToHandshake(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	clientInfoCache, payload := newClientIdentityFixture(t)

	storeSessionClientInfo(ctx, logger, clientInfoCache, payload, "handshake-client", "1.2.3")

	identity := resolveClientIdentity(ctx, logger, clientInfoCache, payload, nil)
	require.Equal(t, toolconfig.MCPClientIdentity{
		Name:          "handshake-client",
		Version:       "1.2.3",
		OAuthClientID: "",
	}, identity)
}

// TestResolveClientIdentity_PerCallHintWins covers the draft stateless model
// (SEP-2575), where a client repeats its identity on every request. That value
// is fresher than the handshake and belongs to a client that may never have
// handshaked at all.
func TestResolveClientIdentity_PerCallHintWins(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	clientInfoCache, payload := newClientIdentityFixture(t)

	storeSessionClientInfo(ctx, logger, clientInfoCache, payload, "handshake-client", "1.2.3")

	identity := resolveClientIdentity(ctx, logger, clientInfoCache, payload, &mcpClientInfoHint{
		Name:    "per-call-client",
		Version: "9.9.9",
	})
	require.Equal(t, "per-call-client", identity.Name)
	require.Equal(t, "9.9.9", identity.Version)
}

// TestResolveClientIdentity_NamelessHintFallsBack keeps a malformed or empty
// per-call hint from erasing a good handshake identity.
func TestResolveClientIdentity_NamelessHintFallsBack(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	clientInfoCache, payload := newClientIdentityFixture(t)

	storeSessionClientInfo(ctx, logger, clientInfoCache, payload, "handshake-client", "1.2.3")

	identity := resolveClientIdentity(ctx, logger, clientInfoCache, payload, &mcpClientInfoHint{
		Name:    "",
		Version: "9.9.9",
	})
	require.Equal(t, "handshake-client", identity.Name)
	require.Equal(t, "1.2.3", identity.Version)
}

func TestResolveClientIdentity_BoundsPerCallHint(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	clientInfoCache, payload := newClientIdentityFixture(t)

	identity := resolveClientIdentity(ctx, logger, clientInfoCache, payload, &mcpClientInfoHint{
		Name:    "ev\x00il\nclient",
		Version: "1.\t0",
	})
	require.Equal(t, "evilclient", identity.Name)
	require.Equal(t, "1.0", identity.Version)
}

// TestResolveClientIdentity_OAuthClientIsIndependent pins the split between the
// two identities: the OAuth client comes from the verified bearer token, so it
// survives whatever the caller reports (or fails to report) about itself.
func TestResolveClientIdentity_OAuthClientIsIndependent(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	clientInfoCache, payload := newClientIdentityFixture(t)
	ctx := contextvalues.SetOAuthClientID(t.Context(), "oauth-client-1")

	identity := resolveClientIdentity(ctx, logger, clientInfoCache, payload, nil)
	require.Empty(t, identity.Name, "no client reported an identity")
	require.Equal(t, "oauth-client-1", identity.OAuthClientID)
}

func TestResolveClientIdentity_UnknownCallerIsZero(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	clientInfoCache, payload := newClientIdentityFixture(t)

	require.True(t, resolveClientIdentity(ctx, logger, clientInfoCache, payload, nil).IsZero())
}
