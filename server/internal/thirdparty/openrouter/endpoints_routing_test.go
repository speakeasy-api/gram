package openrouter

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/partition"
	"github.com/speakeasy-api/gram/server/internal/staff"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// requestLog records the method and path of every request a test server
// receives. Every request is rejected with 400, which neither the guardian
// retry policy nor the SDK retries, so each call reaches a server exactly
// once and the test only asserts on where it landed.
type requestLog struct {
	mu       sync.Mutex
	requests []string
}

func (l *requestLog) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	l.mu.Lock()
	l.requests = append(l.requests, r.Method+" "+r.URL.Path)
	l.mu.Unlock()
	w.WriteHeader(http.StatusBadRequest)
}

func (l *requestLog) has(request string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Contains(l.requests, request)
}

// newRoutingTestServers starts separate inference and management servers so a
// request sent to the wrong host is observable.
func newRoutingTestServers(t *testing.T) (inference, management *requestLog, endpoints Endpoints) {
	t.Helper()

	inference, management = &requestLog{}, &requestLog{}
	inferenceServer := httptest.NewServer(inference)
	t.Cleanup(inferenceServer.Close)
	managementServer := httptest.NewServer(management)
	t.Cleanup(managementServer.Close)

	return inference, management, Endpoints{Inference: inferenceServer.URL, Management: managementServer.URL, ServesDecisionModels: false}
}

func TestProvisionerRoutesRequestsByHost(t *testing.T) {
	t.Parallel()

	inference, management, endpoints := newRoutingTestServers(t)
	tracerProvider := testenv.NewTracerProvider(t)
	policy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	provisioner := New(testenv.NewLogger(t), tracerProvider, policy, nil, "test", partition.EU, "provisioning-key", nil, nil, nil, staff.Organization{})
	provisioner.inferenceURL = endpoints.Inference
	provisioner.managementURL = endpoints.Management

	ctx := t.Context()
	_, err = provisioner.createOpenRouterAPIKey(ctx, "org-1", "acme", KeyTypeChat, 10)
	require.Error(t, err)
	_, err = provisioner.patchOpenRouterAPIKey(ctx, testSpendKeyHash, updateKeyRequest{Limit: nil, LimitReset: "", Disabled: nil})
	require.Error(t, err)
	_, _, err = provisioner.fetchKeyUsage(ctx, provisioner.managementURL+"/v1/keys/"+testSpendKeyHash, provisioner.provisioningKey)
	require.Error(t, err)
	_, err = provisioner.GetDailySpend(ctx, testSpendKeyHash, time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC))
	require.Error(t, err)
	_, _, err = provisioner.GetKeyUsage(ctx, "org-key")
	require.Error(t, err)

	require.True(t, management.has("POST /v1/keys"), "key creation uses the management host")
	require.True(t, management.has("PATCH /v1/keys/"+testSpendKeyHash), "key updates use the management host")
	require.True(t, management.has("GET /v1/keys/"+testSpendKeyHash), "usage by hash uses the management host")
	require.True(t, management.has("POST /v1/analytics/query"), "spend analytics uses the management host")
	require.True(t, inference.has("GET /v1/key"), "usage authorized by the org key uses the inference host")
	require.False(t, inference.has("POST /v1/keys"))
	require.False(t, inference.has("POST /v1/analytics/query"))
	require.False(t, management.has("GET /v1/key"))
}

func TestChatClientRoutesInferenceRequests(t *testing.T) {
	t.Parallel()

	inference, management, endpoints := newRoutingTestServers(t)
	tracerProvider := testenv.NewTracerProvider(t)
	policy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	provisioner := &mockProvisioner{apiKey: "test-api-key"}
	client := NewUnifiedClient(testenv.NewLogger(t), policy, endpoints, provisioner, &PlatformKeyResolver{Provisioner: provisioner}, nil, nil, nil, nil)

	ctx := t.Context()
	resp, err := client.makeHTTPRequest(ctx, "test-api-key", OpenAIChatRequest{Model: DefaultChatModel})
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	_, err = client.CreateEmbeddings(ctx, "org-1", openAITextEmbedding3Small, []string{"hello"})
	require.Error(t, err)
	_, err = client.ListModels(ctx)
	require.Error(t, err)

	require.True(t, inference.has("POST /v1/chat/completions"), "chat completions use the inference host")
	require.True(t, inference.has("POST /v1/embeddings"), "embeddings use the inference host")
	require.True(t, inference.has("GET /v1/models"), "model discovery uses the inference host")
	require.Empty(t, management.requests)
}
