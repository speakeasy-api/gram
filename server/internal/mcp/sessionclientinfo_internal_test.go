package mcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/sessionclientinfo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

// fakeClientInfoStore records identities in memory so resolution precedence can
// be exercised without Redis. The Redis-backed behaviour (scoping, eviction)
// has its own tests in the sessionclientinfo package.
type fakeClientInfoStore struct {
	records map[string]sessionclientinfo.Info
}

func newFakeClientInfoStore() *fakeClientInfoStore {
	return &fakeClientInfoStore{records: map[string]sessionclientinfo.Info{}}
}

func (f *fakeClientInfoStore) key(projectID uuid.UUID, toolsetSlug, sessionID string) string {
	return projectID.String() + ":" + toolsetSlug + ":" + sessionID
}

func (f *fakeClientInfoStore) Store(_ context.Context, projectID uuid.UUID, toolsetSlug, sessionID string, info sessionclientinfo.Info, _ int64) error {
	f.records[f.key(projectID, toolsetSlug, sessionID)] = info
	return nil
}

func (f *fakeClientInfoStore) Load(_ context.Context, projectID uuid.UUID, toolsetSlug, sessionID string, _ int64) (sessionclientinfo.Info, error) {
	info, ok := f.records[f.key(projectID, toolsetSlug, sessionID)]
	if !ok {
		return sessionclientinfo.Info{}, sessionclientinfo.ErrNotFound
	}
	return info, nil
}

func newClientIdentityFixture(t *testing.T) (*fakeClientInfoStore, *mcpInputs) {
	t.Helper()

	return newFakeClientInfoStore(), &mcpInputs{
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

func TestResolveClientIdentity_FallsBackToHandshake(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	store, payload := newClientIdentityFixture(t)

	storeSessionClientInfo(ctx, logger, store, payload, "handshake-client", "1.2.3")

	identity := resolveClientIdentity(ctx, logger, store, payload, nil)
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
	store, payload := newClientIdentityFixture(t)

	storeSessionClientInfo(ctx, logger, store, payload, "handshake-client", "1.2.3")

	identity := resolveClientIdentity(ctx, logger, store, payload, &mcpClientInfoHint{
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
	store, payload := newClientIdentityFixture(t)

	storeSessionClientInfo(ctx, logger, store, payload, "handshake-client", "1.2.3")

	identity := resolveClientIdentity(ctx, logger, store, payload, &mcpClientInfoHint{
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
	store, payload := newClientIdentityFixture(t)

	identity := resolveClientIdentity(ctx, logger, store, payload, &mcpClientInfoHint{
		Name:    "ev\x00il\nclient",
		Version: "1.\t0",
	})
	require.Equal(t, "evilclient", identity.Name)
	require.Equal(t, "1.0", identity.Version)
}

// TestStoreSessionClientInfo_BoundsRecordedFields pins that the untrusted
// handshake values are cleaned before they are recorded, not just on the way
// out.
func TestStoreSessionClientInfo_BoundsRecordedFields(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	store, payload := newClientIdentityFixture(t)

	storeSessionClientInfo(ctx, logger, store, payload, "ev\x00il\nclient", "1.\t0")

	info, err := store.Load(ctx, payload.projectID, payload.toolset, payload.sessionID, 0)
	require.NoError(t, err)
	require.Equal(t, "evilclient", info.Name)
	require.Equal(t, "1.0", info.Version)
}

func TestStoreSessionClientInfo_NamelessClientRecordsNothing(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	store, payload := newClientIdentityFixture(t)

	storeSessionClientInfo(ctx, logger, store, payload, "", "1.2.3")

	_, err := store.Load(ctx, payload.projectID, payload.toolset, payload.sessionID, 0)
	require.ErrorIs(t, err, sessionclientinfo.ErrNotFound)
}

// TestResolveClientIdentity_OAuthClientIsIndependent pins the split between the
// two identities: the OAuth client comes from the verified bearer token, so it
// survives whatever the caller reports (or fails to report) about itself.
func TestResolveClientIdentity_OAuthClientIsIndependent(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	store, payload := newClientIdentityFixture(t)
	ctx := contextvalues.SetOAuthClientID(t.Context(), "oauth-client-1")

	identity := resolveClientIdentity(ctx, logger, store, payload, nil)
	require.Empty(t, identity.Name, "no client reported an identity")
	require.Equal(t, "oauth-client-1", identity.OAuthClientID)
}

func TestResolveClientIdentity_UnknownCallerIsZero(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	store, payload := newClientIdentityFixture(t)

	require.True(t, resolveClientIdentity(ctx, logger, store, payload, nil).IsZero())
}
