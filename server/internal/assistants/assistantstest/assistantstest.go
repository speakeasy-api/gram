// Package assistantstest builds a fully wired assistants.ServiceCore for tests
// in other packages.
package assistantstest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/superfly/fly-go/tokens"

	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/assistants"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/chat/chattest"
	"github.com/speakeasy-api/gram/server/internal/environments"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	mcpmetadatarepo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	slackapi "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/api"
	slackclient "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/client"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

const signingKey = "test-jwt-secret"

type staticContextWindow struct{ tokens int }

func (s staticContextWindow) Resolve(context.Context, string) (int, error) { return s.tokens, nil }

// NewServiceCore returns an assistants.ServiceCore with every dependency
// constructed for real. The runtime backend is the Fly backend pointed at a
// fake Fly API that rejects every request, so nothing reaches a real runtime
// provider.
func NewServiceCore(t *testing.T, env *testenv.Environment, conn *pgxpool.Pool) *assistants.ServiceCore {
	t.Helper()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, nil)
	require.NoError(t, err)
	enc := testenv.NewEncryptionClient(t)
	storage := assetstest.NewTestBlobStore(t)
	authzEngine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	envEntries := environments.NewEnvironmentEntries(logger, conn, enc, mcpmetadatarepo.New(conn))
	chatWriter, chatWriterShutdown := chat.NewChatMessageWriter(logger, conn, storage, chattest.NewTurnStream(t, env))
	t.Cleanup(func() { _ = chatWriterShutdown(context.Background()) })

	serverURL := &url.URL{Scheme: "https", Host: "gram.example.com"}

	fakeFly := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "fake fly api", http.StatusServiceUnavailable)
	}))
	t.Cleanup(fakeFly.Close)
	runtime := assistants.NewRuntimeBackend(logger, tracerProvider, guardianPolicy, assistants.RuntimeBackendConfig{
		Provider: assistants.RuntimeProviderFlyIO,
		Fly: assistants.FlyRuntimeConfig{
			ServiceName:        "",
			ServiceVersion:     "",
			FlyTokens:          tokens.Parse("test-fly-token"),
			FlyAPIURL:          fakeFly.URL,
			FlyMachinesBaseURL: fakeFly.URL,
			DefaultFlyOrg:      "test-org",
			DefaultFlyRegion:   "",
			OCIImage:           "gram-assistant-runtime",
			ImageTag:           "test",
			AppNamePrefix:      "",
			ServerURL:          serverURL,
			OTLPEndpoint:       "",
			OTLPProtocol:       "",
			OTLPHeaders:        "",
			Environment:        "",
		},
		GKE: assistants.GKERuntimeConfig{
			Dynamic:          nil,
			Namespace:        "",
			SandboxTemplate:  "",
			GuestPort:        0,
			OCIImage:         "",
			ImageTag:         "",
			ServerURL:        nil,
			RunnerCIDRBlocks: nil,
		},
		Local: assistants.LocalRuntimeConfig{
			Enabled:         false,
			Environment:     "",
			OCIImage:        "",
			ImageTag:        "",
			GuestPort:       0,
			ServerURL:       nil,
			ExtraCACertFile: "",
		},
	})

	slackClient := slackclient.NewSlackClient(guardianPolicy)
	core := assistants.NewServiceCore(
		logger,
		tracerProvider,
		testenv.NewMeterProvider(t),
		conn,
		guardianPolicy,
		enc,
		runtime,
		slackClient,
		assistanttokens.New(signingKey, conn, authzEngine),
		serverURL,
		telemetry.NewStub(logger),
		staticContextWindow{tokens: 0},
		audit.NewLogger(),
		chatWriter,
		storage,
		signingKey,
		envEntries,
		slackapi.NewClient("", guardianPolicy.PooledClient()),
		&feature.InMemory{},
	)
	temporalEnv, _ := env.NewTemporalEnv(t)
	triggerApp := bgtriggers.NewApp(
		logger,
		conn,
		temporalEnv,
		envEntries,
		bgtriggers.NewTriggerDeliveryLogger(func(context.Context, bgtriggers.TriggerDeliveryLog) {}),
		audit.NewLogger(),
		serverURL,
		nil,
		nil,
		slackClient,
		testenv.NewMemoryCache(),
	)
	core.SetWakeCanceller(triggerApp)
	core.SetDashboardIngestor(triggerApp)
	return core
}
