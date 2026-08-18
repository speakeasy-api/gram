package mcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcprequests"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
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
		protocolVersionHeader: "",
	}
}

func TestResolveClientIdentity_FallsBackToHandshake(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	store, payload := newClientIdentityFixture(t)

	storeSessionClientInfo(ctx, logger, store, payload, "handshake-client", "1.2.3", mcpversions.Version20250618)

	identity, storedProtocolVersion := resolveClientIdentity(ctx, logger, store, payload, nil)
	require.Equal(t, toolconfig.MCPClientIdentity{
		Name:          "handshake-client",
		Version:       "1.2.3",
		OAuthClientID: "",
	}, identity)
	require.Equal(t, mcpversions.Version20250618, storedProtocolVersion,
		"the stored handshake version rides along for per-request telemetry enrichment")
}

// TestResolveClientIdentity_HintPathReportsNoStoredVersion pins that the
// stored protocol version is only reported when the identity actually came
// from the stored record: a client on the per-request hint path declares its
// version on the request itself, so callers already have a better source.
func TestResolveClientIdentity_HintPathReportsNoStoredVersion(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	store, payload := newClientIdentityFixture(t)

	storeSessionClientInfo(ctx, logger, store, payload, "handshake-client", "1.2.3", mcpversions.Version20250618)

	_, storedProtocolVersion := resolveClientIdentity(ctx, logger, store, payload, &mcprequests.SanitizedClientInfo{
		Name:    "per-call-client",
		Version: "9.9.9",
	})
	require.Empty(t, storedProtocolVersion)
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

	storeSessionClientInfo(ctx, logger, store, payload, "handshake-client", "1.2.3", mcpversions.Version20250618)

	identity, _ := resolveClientIdentity(ctx, logger, store, payload, &mcprequests.SanitizedClientInfo{
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

	storeSessionClientInfo(ctx, logger, store, payload, "handshake-client", "1.2.3", mcpversions.Version20250618)

	identity, _ := resolveClientIdentity(ctx, logger, store, payload, &mcprequests.SanitizedClientInfo{
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

	identity, _ := resolveClientIdentity(ctx, logger, store, payload, &mcprequests.SanitizedClientInfo{
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

	storeSessionClientInfo(ctx, logger, store, payload, "ev\x00il\nclient", "1.\t0", "2025-06-18\x00injected")

	info, err := store.Load(ctx, payload.projectID, payload.toolset, payload.sessionID, 0)
	require.NoError(t, err)
	require.Equal(t, "evilclient", info.Name)
	require.Equal(t, "1.0", info.Version)
	require.Empty(t, info.ProtocolVersion, "a protocol version carrying control bytes is dropped, not cleaned")
}

func TestStoreSessionClientInfo_AnonymousClientRecordsNothing(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	store, payload := newClientIdentityFixture(t)

	storeSessionClientInfo(ctx, logger, store, payload, "", "1.2.3", "")

	_, err := store.Load(ctx, payload.projectID, payload.toolset, payload.sessionID, 0)
	require.ErrorIs(t, err, sessionclientinfo.ErrNotFound)
}

// TestStoreSessionClientInfo_NamelessClientStillRecordsProtocolVersion pins
// that a protocol version alone is enough to earn a record: a client that omits
// clientInfo.name stays attributable to a protocol generation for the rest of
// its session.
func TestStoreSessionClientInfo_NamelessClientStillRecordsProtocolVersion(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	store, payload := newClientIdentityFixture(t)

	storeSessionClientInfo(ctx, logger, store, payload, "", "", mcpversions.Version20250618)

	info, err := store.Load(ctx, payload.projectID, payload.toolset, payload.sessionID, 0)
	require.NoError(t, err)
	require.Empty(t, info.Name)
	require.Equal(t, mcpversions.Version20250618, info.ProtocolVersion)
}

func TestStoreSessionClientInfo_RecordsProtocolVersionAlongsideIdentity(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	store, payload := newClientIdentityFixture(t)

	storeSessionClientInfo(ctx, logger, store, payload, "handshake-client", "1.2.3", mcpversions.Version20251125)

	info, err := store.Load(ctx, payload.projectID, payload.toolset, payload.sessionID, 0)
	require.NoError(t, err)
	require.Equal(t, mcpversions.Version20251125, info.ProtocolVersion)
}

// TestResolveClientIdentity_OAuthClientIsIndependent pins the split between the
// two identities: the OAuth client comes from the verified bearer token, so it
// survives whatever the caller reports (or fails to report) about itself.
func TestResolveClientIdentity_OAuthClientIsIndependent(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	store, payload := newClientIdentityFixture(t)
	ctx := contextvalues.SetOAuthClientID(t.Context(), "oauth-client-1")

	identity, _ := resolveClientIdentity(ctx, logger, store, payload, nil)
	require.Empty(t, identity.Name, "no client reported an identity")
	require.Equal(t, "oauth-client-1", identity.OAuthClientID)
}

func TestResolveClientIdentity_UnknownCallerIsZero(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	store, payload := newClientIdentityFixture(t)

	identity, storedProtocolVersion := resolveClientIdentity(ctx, logger, store, payload, nil)
	require.True(t, identity.IsZero())
	require.Empty(t, storedProtocolVersion)
}

// TestResolveClientIdentity_StampsBothProtocolVersions covers the cohort this
// propagation exists for: a client predating the MCP-Protocol-Version header
// gets no attribute from middleware, so this is the only place either half of
// the protocol version reaches the span on a tool call.
func TestResolveClientIdentity_StampsBothProtocolVersions(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	store, payload := newClientIdentityFixture(t)

	storeSessionClientInfo(ctx, logger, store, payload, "handshake-client", "1.2.3", mcpversions.Version20241105)

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	spanCtx, span := provider.Tracer("test").Start(ctx, "tools/call")
	resolveClientIdentity(spanCtx, logger, store, payload, nil)
	span.End()

	ended := recorder.Ended()
	require.Len(t, ended, 1)

	got := map[string]string{}
	for _, kv := range ended[0].Attributes() {
		got[string(kv.Key)] = kv.Value.AsString()
	}

	require.Equal(t, mcpversions.Version20241105, got[string(attr.McpRequestedProtocolVersionKey)])
	require.Equal(t, mcpversions.ServedHostedToolset, got[string(attr.McpNegotiatedProtocolVersionKey)],
		"this surface answers its served constant, so the negotiated half is deterministic here")
}
