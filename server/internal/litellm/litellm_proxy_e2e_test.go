//go:build litellm_e2e

package litellm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.temporal.io/sdk/testsuite"
	goahttp "goa.design/goa/v3/http"
	"gopkg.in/yaml.v3"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	gen "github.com/speakeasy-api/gram/server/gen/litellm"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/authz"
	riskanalysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/killswitches/mcptoolexecution"
	"github.com/speakeasy-api/gram/server/internal/litellmacting"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/risk"
	riskcelenv "github.com/speakeasy-api/gram/server/internal/risk/celenv"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/scanners/customruleanalyzer"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	fixtureAnswer       = "fixture answer"
	proxyMasterKey      = "fixture-master-key"
	policyBlockedReason = "Synthetic secrets are not permitted."
	syntheticSecret     = "ghp_R2D2C3POLuk3Skywalker1234567890ab"
	proxyRequestTimeout = 3 * time.Second
)

type proxyManifest struct {
	Image string `json:"image"`
}

type proxyGuardrail struct {
	Name   string         `yaml:"guardrail_name"`
	Params map[string]any `yaml:"litellm_params"`
}

type canonicalGuardrails struct {
	Guardrails []proxyGuardrail `yaml:"guardrails"`
}

type proxyConfig struct {
	Models          []proxyModel     `yaml:"model_list"`
	GeneralSettings map[string]any   `yaml:"general_settings"`
	Guardrails      []proxyGuardrail `yaml:"guardrails"`
}

type proxyModel struct {
	Name   string         `yaml:"model_name"`
	Params map[string]any `yaml:"litellm_params"`
}

type providerCallKey struct {
	Scenario string
	Session  string
	CallID   string
}

type fixtureProvider struct {
	mu    sync.Mutex
	calls map[providerCallKey]int
}

func (p *fixtureProvider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var request struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		Stream bool `json:"stream"`
	}
	if err := json.Unmarshal(body, &request); err != nil || len(request.Messages) == 0 {
		http.Error(w, "invalid fixture request", http.StatusBadRequest)
		return
	}
	fields := strings.Fields(request.Messages[len(request.Messages)-1].Content)
	if len(fields) < 3 {
		http.Error(w, "missing fixture call key", http.StatusBadRequest)
		return
	}
	key := providerCallKey{
		Scenario: strings.TrimPrefix(fields[0], "scenario="),
		Session:  strings.TrimPrefix(fields[1], "session="),
		CallID:   strings.TrimPrefix(fields[2], "call="),
	}
	p.mu.Lock()
	p.calls[key]++
	p.mu.Unlock()

	if request.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		chunks := []string{"fixture ", "answer"}
		for index, text := range chunks {
			finishReason := any(nil)
			if index == len(chunks)-1 {
				finishReason = "stop"
			}
			chunk, marshalErr := json.Marshal(map[string]any{
				"id": "chatcmpl-fixture-stream", "object": "chat.completion.chunk", "created": 1,
				"model": "fixture-openai", "choices": []any{map[string]any{
					"index": 0, "delta": map[string]any{"content": text}, "finish_reason": finishReason,
				}},
			})
			if marshalErr != nil {
				return
			}
			_, _ = fmt.Fprintf(w, "data: %s\n\n", chunk)
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id": "chatcmpl-fixture", "object": "chat.completion", "created": 1, "model": "fixture-openai",
		"choices": []any{map[string]any{
			"index": 0, "message": map[string]any{"role": "assistant", "content": fixtureAnswer}, "finish_reason": "stop",
		}},
		"usage": map[string]any{"prompt_tokens": 4, "completion_tokens": 2, "total_tokens": 6},
	})
}

func (p *fixtureProvider) count(key providerCallKey) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls[key]
}

type timeoutRelay struct {
	gramURL       string
	apiKey        string
	projectSlug   string
	timeoutCallID string
	release       chan struct{}
	releaseOnce   sync.Once
	mu            sync.Mutex
	preCalls      int
	waiting       bool
}

func (r *timeoutRelay) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var callback struct {
		InputType string `json:"input_type"`
		CallID    string `json:"litellm_call_id"`
	}
	if err := json.Unmarshal(body, &callback); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	forward, err := http.NewRequestWithContext(req.Context(), http.MethodPost, r.gramURL+req.URL.Path, bytes.NewReader(body))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	forward.Header.Set("Content-Type", "application/json")
	forward.Header.Set("Gram-Key", r.apiKey)
	forward.Header.Set("Gram-Project", r.projectSlug)
	response, err := http.DefaultClient.Do(forward)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	responseBody, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil {
		http.Error(w, "read Gram response", http.StatusBadGateway)
		return
	}

	if callback.InputType == "request" && callback.CallID == r.timeoutCallID {
		r.mu.Lock()
		r.preCalls++
		first := r.preCalls == 1
		r.mu.Unlock()
		if first {
			r.mu.Lock()
			r.waiting = true
			r.mu.Unlock()
			timer := time.NewTimer(proxyRequestTimeout + 500*time.Millisecond)
			select {
			case <-r.release:
			case <-timer.C:
			}
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			http.Error(w, "synthetic delayed guardrail response", http.StatusServiceUnavailable)
			r.mu.Lock()
			r.waiting = false
			r.mu.Unlock()
			return
		}
	}
	for name, values := range response.Header {
		w.Header()[name] = values
	}
	w.WriteHeader(response.StatusCode)
	_, _ = w.Write(responseBody)
}

func (r *timeoutRelay) isWaiting() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.waiting
}

func (r *timeoutRelay) releaseFirst() {
	r.releaseOnce.Do(func() { close(r.release) })
}

type proxyHarness struct {
	t          *testing.T
	ctx        context.Context
	conn       *pgxpool.Pool
	service    *Service
	signer     *litellmacting.Signer
	lifecycle  *killswitches.LifecycleService
	projectID  uuid.UUID
	orgID      string
	userID     string
	instanceID string
	apiKeyID   string
	policy     riskrepo.RiskPolicy
	proxyURL   string
	provider   *fixtureProvider
	relay      *timeoutRelay
	httpClient *http.Client
}

type proxyResponse struct {
	Status int
	Header http.Header
	Body   []byte
}

type proxyRequestOptions struct {
	scenario          string
	sessionHeader     string
	sessionID         string
	callID            string
	guardrail         string
	text              string
	stream            bool
	actingHeaders     map[string]string
	additionalHeaders []map[string]string
}

func TestLiteLLMProxyE2E(t *testing.T) { //nolint:paralleltest // Scenarios intentionally share one proxy container.
	test := newProxyHarness(t)
	test.safeNonStreaming()
	test.nativeSessionHeaders()
	test.blockedNonStreaming()
	test.aiAccessHeadersBlockBeforeProvider()
	test.safeStreaming()
	test.blockedStreaming()
	test.failClosed()
	test.failOpen()
	test.timeoutAndResend()
}

func newProxyHarness(t *testing.T) *proxyHarness {
	t.Helper()
	ctx, instance := newRealTestServiceWithScannerFactory(t, func(conn *pgxpool.Pool) risk.RiskScanner {
		customRules, err := customruleanalyzer.NewScanner(conn)
		require.NoError(t, err)
		celEngine, err := riskcelenv.New()
		require.NoError(t, err)
		scanner, err := risk.NewScanner(
			testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn,
			customRules, nil, nil, nil, &feature.InMemory{}, celEngine,
		)
		require.NoError(t, err)
		return scanner
	})
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	require.NotNil(t, authCtx.ProjectSlug)

	policyID := uuid.New()
	policy, err := riskrepo.New(instance.conn).CreateRiskPolicy(ctx, riskrepo.CreateRiskPolicyParams{
		ID:                   policyID,
		ProjectID:            *authCtx.ProjectID,
		OrganizationID:       authCtx.ActiveOrganizationID,
		Name:                 "synthetic secret block",
		PolicyType:           nil,
		Sources:              []string{riskanalysis.SourceGitleaks},
		PresidioEntities:     nil,
		AnalyzerConfig:       nil,
		PromptInjectionRules: nil,
		DisabledRules:        nil,
		CustomRuleIds:        nil,
		MessageTypes:         []string{message.User},
		ScopeInclude:         pgtype.Text{},
		ScopeExempt:          pgtype.Text{},
		Enabled:              true,
		Action:               "block",
		AudienceType:         "everyone",
		ShadowMcpDisposition: pgtype.Text{},
		AutoName:             false,
		UserMessage:          pgtype.Text{String: policyBlockedReason, Valid: true},
		Prompt:               pgtype.Text{},
		ModelConfig:          nil,
		Score:                pgtype.Float8{},
	})
	require.NoError(t, err)
	require.NoError(t, authz.ReplaceGrantsForResource(ctx, instance.conn, authz.ResourceGrant{
		Resource: authz.Resource{
			OrganizationID: authCtx.ActiveOrganizationID,
			Scope:          authz.ScopeRiskPolicyEvaluate,
			ResourceID:     policyID.String(),
		},
		Principals: []urn.Principal{authz.AllUsersPrincipal()},
		Selector:   nil,
	}))

	signer, err := litellmacting.NewSigner("litellm-proxy-e2e-secret")
	require.NoError(t, err)
	registry, err := mcptoolexecution.NewRegistry(instance.conn)
	require.NoError(t, err)
	evaluator, err := killswitches.NewEvaluator(instance.conn, registry, liteLLMAIAccessTimeout, testenv.NewMeterProvider(t), testenv.NewLogger(t))
	require.NoError(t, err)
	checkpoint, err := NewLiteLLMAIAccessCheckpoint(registry, evaluator, signer)
	require.NoError(t, err)
	lifecycle, err := killswitches.NewLifecycleService(instance.conn, registry, mcptoolexecution.NewCustomerLifecycleValidator(), nil)
	require.NoError(t, err)
	managed, err := instance.service.CreateInstance(ctx, &gen.CreateInstancePayload{Name: "litellm-proxy-e2e", FailurePosture: "fail_closed"})
	require.NoError(t, err)
	keyHash, err := auth.GetAPIKeyHash(managed.Key)
	require.NoError(t, err)
	managedKey, err := keysrepo.New(instance.conn).GetAPIKeyByKeyHash(ctx, keyHash)
	require.NoError(t, err)
	instance.service.actingSigner = signer
	instance.service.aiAccess = checkpoint
	mux := goahttp.NewMuxer()
	Attach(mux, instance.service)
	gramServer := httptest.NewServer(mux)
	t.Cleanup(gramServer.Close)

	provider := &fixtureProvider{mu: sync.Mutex{}, calls: make(map[providerCallKey]int)}
	providerServer := httptest.NewServer(provider)
	t.Cleanup(providerServer.Close)
	// Keep the resend on a fresh Docker Desktop host-port connection after the
	// intentional guardrail read timeout, while using the same fixture provider.
	timeoutProviderServer := httptest.NewServer(provider)
	t.Cleanup(timeoutProviderServer.Close)
	failureServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "synthetic guardrail outage", http.StatusServiceUnavailable)
	}))
	t.Cleanup(failureServer.Close)

	timeoutCallID := "e2e-timeout-call"
	relay := &timeoutRelay{
		gramURL:       gramServer.URL + "/rpc/litellm.ingest",
		apiKey:        managed.Key,
		projectSlug:   *authCtx.ProjectSlug,
		timeoutCallID: timeoutCallID,
		release:       make(chan struct{}),
		releaseOnce:   sync.Once{},
		mu:            sync.Mutex{},
		preCalls:      0,
		waiting:       false,
	}
	relayServer := httptest.NewServer(relay)
	t.Cleanup(func() {
		relay.releaseFirst()
		relayServer.Close()
	})

	config := buildProxyConfig(t,
		containerURL(t, providerServer.URL)+"/v1",
		containerURL(t, timeoutProviderServer.URL)+"/v1",
		containerURL(t, gramServer.URL)+"/rpc/litellm.ingest",
		containerURL(t, failureServer.URL),
		containerURL(t, relayServer.URL),
	)
	manifestBytes := testenv.ReadFixture(t, contractFixtureDir+"manifest.json")
	var manifest proxyManifest
	require.NoError(t, json.Unmarshal(manifestBytes, &manifest))
	require.NotEmpty(t, manifest.Image)

	configBytes, err := yaml.Marshal(config)
	require.NoError(t, err)
	hostPorts := []int{
		serverPort(t, gramServer.URL), serverPort(t, providerServer.URL), serverPort(t, timeoutProviderServer.URL),
		serverPort(t, failureServer.URL), serverPort(t, relayServer.URL),
	}
	container, err := testcontainers.Run(t.Context(), manifest.Image,
		testcontainers.WithLogger(testenv.NewTestcontainersLogger()),
		testcontainers.WithExposedPorts("4000/tcp"),
		testcontainers.WithHostPortAccess(hostPorts...),
		testcontainers.WithFiles(testcontainers.ContainerFile{
			Reader: bytes.NewReader(configBytes), ContainerFilePath: "/app/e2e-config.yaml", FileMode: 0o644,
		}),
		testcontainers.WithEnv(map[string]string{
			"GRAM_LITELLM_INGEST_KEY": managed.Key,
			"GRAM_PROJECT_SLUG":       *authCtx.ProjectSlug,
			"REQUEST_TIMEOUT":         fmt.Sprintf("%d", proxyRequestTimeout/time.Second),
		}),
		testcontainers.WithCmd("--config", "/app/e2e-config.yaml", "--port", "4000"),
		testcontainers.WithWaitStrategy(wait.ForHTTP("/health/liveliness").WithPort("4000/tcp").WithStartupTimeout(3*time.Minute)),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		if t.Failed() {
			logs, logsErr := container.Logs(context.Background())
			if logsErr == nil {
				logBody, readErr := io.ReadAll(logs)
				if readErr == nil {
					t.Logf("LiteLLM logs:\n%s", logBody)
				}
				_ = logs.Close()
			}
		}
		require.NoError(t, container.Terminate(context.Background()))
	})
	proxyURL, err := container.Endpoint(t.Context(), "http")
	require.NoError(t, err)

	return &proxyHarness{
		t: t, ctx: ctx, conn: instance.conn, service: instance.service, signer: signer, lifecycle: lifecycle,
		projectID: *authCtx.ProjectID, orgID: authCtx.ActiveOrganizationID, userID: authCtx.UserID,
		instanceID: managed.Instance.ID, apiKeyID: managedKey.ID.String(), policy: policy, proxyURL: proxyURL,
		provider: provider, relay: relay, httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

func buildProxyConfig(t *testing.T, providerURL, timeoutProviderURL, gramURL, failureURL, relayURL string) proxyConfig {
	t.Helper()
	readme := string(testenv.ReadFixture(t, contractFixtureDir+"README.md"))
	marker := "The canonical guardrail stanza is:"
	start := strings.Index(readme, marker)
	require.NotEqual(t, -1, start)
	fenceStart := strings.Index(readme[start:], "```yaml\n")
	require.NotEqual(t, -1, fenceStart)
	fenceStart += start + len("```yaml\n")
	fenceEnd := strings.Index(readme[fenceStart:], "\n```")
	require.NotEqual(t, -1, fenceEnd)

	var canonical canonicalGuardrails
	require.NoError(t, yaml.Unmarshal([]byte(readme[fenceStart:fenceStart+fenceEnd]), &canonical))
	require.Len(t, canonical.Guardrails, 1)
	guardrail := canonical.Guardrails[0]
	require.Equal(t, "gram", guardrail.Name)
	require.Equal(t, "generic_guardrail_api", guardrail.Params["guardrail"])
	require.Equal(t, true, guardrail.Params["default_on"])
	require.Equal(t, true, guardrail.Params["fail_on_error"])
	require.Equal(t, "fail_closed", guardrail.Params["unreachable_fallback"])
	require.Equal(t, true, guardrail.Params["streaming_end_of_stream_only"])
	require.NotEmpty(t, guardrail.Params["api_base"])
	require.ElementsMatch(t, []any{"pre_call", "post_call"}, guardrail.Params["mode"])
	require.Equal(t, []any{
		"x-gram-acting-principal",
		"x-gram-acting-principal-contract",
		"x-gram-inference-invocation-id",
		"x-gram-session-id",
		"x-claude-code-session-id",
		"session-id",
		"thread-id",
		"x-session-id",
	}, guardrail.Params["extra_headers"])
	headers, ok := guardrail.Params["headers"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "os.environ/GRAM_LITELLM_INGEST_KEY", headers["Gram-Key"])
	require.Equal(t, "os.environ/GRAM_PROJECT_SLUG", headers["Gram-Project"])

	clone := func(name, apiBase string, failureBehavior bool) proxyGuardrail {
		params := maps.Clone(guardrail.Params)
		params["api_base"] = apiBase
		params["default_on"] = false
		if failureBehavior {
			params["fail_on_error"] = true
		}
		return proxyGuardrail{Name: name, Params: params}
	}
	normal := clone("gram-e2e", gramURL, false)
	failClosed := clone("gram-e2e-fail-closed", failureURL, true)
	failOpen := clone("gram-e2e-fail-open", failureURL, true)
	failOpen.Params["unreachable_fallback"] = "fail_open"
	timeout := clone("gram-e2e-timeout", relayURL, true)

	return proxyConfig{
		Models: []proxyModel{
			{
				Name: "fixture-openai",
				Params: map[string]any{
					"model": "openai/fixture-openai", "api_base": providerURL, "api_key": "fixture-provider-key", "num_retries": 0,
				},
			},
			{
				Name: "fixture-timeout",
				Params: map[string]any{
					"model": "openai/fixture-timeout", "api_base": timeoutProviderURL, "api_key": "fixture-provider-key", "num_retries": 0,
				},
			},
		},
		GeneralSettings: map[string]any{"master_key": proxyMasterKey},
		Guardrails:      []proxyGuardrail{normal, failClosed, failOpen, timeout},
	}
}

func containerURL(t *testing.T, rawURL string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	require.NoError(t, err)
	return "http://host.testcontainers.internal:" + parsed.Port()
}

func serverPort(t *testing.T, rawURL string) int {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	require.NoError(t, err)
	_, port, err := net.SplitHostPort(parsed.Host)
	require.NoError(t, err)
	var value int
	_, err = fmt.Sscanf(port, "%d", &value)
	require.NoError(t, err)
	return value
}

func (h *proxyHarness) request(scenario, sessionHeader, sessionID, callID, guardrail, text string, stream bool, additionalSessionHeaders ...map[string]string) proxyResponse {
	h.t.Helper()
	return h.requestWithOptions(proxyRequestOptions{
		scenario: scenario, sessionHeader: sessionHeader, sessionID: sessionID, callID: callID, guardrail: guardrail, text: text, stream: stream,
		actingHeaders: h.mintActingHeaders(), additionalHeaders: additionalSessionHeaders,
	})
}

func (h *proxyHarness) requestWithOptions(options proxyRequestOptions) proxyResponse {
	h.t.Helper()
	prompt := h.prompt(options.scenario, options.sessionID, options.callID, options.text)
	model := "fixture-openai"
	if options.scenario == "timeout" {
		model = "fixture-timeout"
	}
	body, err := json.Marshal(map[string]any{
		"model": model, "messages": []any{map[string]any{"role": "user", "content": prompt}},
		"guardrails": []string{options.guardrail}, "stream": options.stream,
	})
	require.NoError(h.t, err)
	req, err := http.NewRequestWithContext(h.t.Context(), http.MethodPost, h.proxyURL+"/v1/chat/completions", bytes.NewReader(body))
	require.NoError(h.t, err)
	req.Header.Set("Authorization", "Bearer "+proxyMasterKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(options.sessionHeader, options.sessionID)
	for name, value := range options.actingHeaders {
		req.Header.Set(name, value)
	}
	for _, headers := range options.additionalHeaders {
		for name, value := range headers {
			req.Header.Set(name, value)
		}
	}
	req.Header.Set("x-litellm-call-id", options.callID)
	response, err := h.httpClient.Do(req)
	require.NoError(h.t, err)
	responseBody, err := io.ReadAll(response.Body)
	require.NoError(h.t, err)
	require.NoError(h.t, response.Body.Close())
	return proxyResponse{Status: response.StatusCode, Header: response.Header.Clone(), Body: responseBody}
}

func (h *proxyHarness) mintActingHeaders() map[string]string {
	h.t.Helper()
	invocationID, err := uuid.NewV7()
	require.NoError(h.t, err)
	minted, err := h.service.MintActingPrincipal(h.ctx, &gen.MintActingPrincipalPayload{
		InstanceID: h.instanceID, InvocationID: invocationID.String(),
	})
	require.NoError(h.t, err)
	return map[string]string{
		actingPrincipalHeader: minted.Assertion, actingPrincipalContractHeader: minted.ContractVersion, inferenceInvocationHeader: minted.InvocationID,
	}
}

func (h *proxyHarness) key(scenario, sessionID, callID string) providerCallKey {
	return providerCallKey{Scenario: scenario, Session: sessionID, CallID: callID}
}

func (h *proxyHarness) prompt(scenario, sessionID, callID, text string) string {
	return fmt.Sprintf("scenario=%s session=%s call=%s %s", scenario, sessionID, callID, text)
}

func (h *proxyHarness) messages(sessionID string, count int) []chatrepo.ChatMessage {
	h.t.Helper()
	return requireChatMessages(h.t, h.t.Context(), h.conn, chatrepo.ListChatMessagesParams{
		ChatID: chat.SessionIDToChatID(sessionID), ProjectID: h.projectID,
	}, count)
}

func (h *proxyHarness) noMessages(sessionID string) {
	h.t.Helper()
	messages, err := chatrepo.New(h.conn).ListChatMessages(h.t.Context(), chatrepo.ListChatMessagesParams{
		ChatID: chat.SessionIDToChatID(sessionID), ProjectID: h.projectID,
	})
	require.NoError(h.t, err)
	require.Empty(h.t, messages)
}

func (h *proxyHarness) requireConversation(messages []chatrepo.ChatMessage, prompt string) {
	h.t.Helper()
	require.Equal(h.t, []string{"user", "assistant"}, []string{messages[0].Role, messages[1].Role})
	require.Equal(h.t, prompt, messages[0].Content)
	require.Equal(h.t, fixtureAnswer, messages[1].Content)
	for _, stored := range messages {
		require.Equal(h.t, "litellm", stored.Source.String)
	}
}

func (h *proxyHarness) requireCompletion(body []byte) {
	h.t.Helper()
	var completion struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	require.NoError(h.t, json.Unmarshal(body, &completion))
	require.Len(h.t, completion.Choices, 1)
	require.Equal(h.t, fixtureAnswer, completion.Choices[0].Message.Content)
}

func (h *proxyHarness) requireBlocked(body []byte) {
	h.t.Helper()
	require.Equal(h.t, policyBlockedReason, blockedMessage(h.t, body))
}

func blockedMessage(t *testing.T, body []byte) string {
	t.Helper()
	var response struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(body, &response))
	return response.Error.Message
}

func (h *proxyHarness) requireSSE(body []byte) {
	h.t.Helper()
	var answer strings.Builder
	done := false
	scanner := bufio.NewScanner(bytes.NewReader(body))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			done = true
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		require.NoError(h.t, json.Unmarshal([]byte(data), &chunk))
		require.Len(h.t, chunk.Choices, 1)
		answer.WriteString(chunk.Choices[0].Delta.Content)
	}
	require.NoError(h.t, scanner.Err())
	require.True(h.t, done)
	require.Equal(h.t, fixtureAnswer, answer.String())
}

func (h *proxyHarness) safeNonStreaming() {
	scenario, sessionID, callID := "safe", "e2e-safe-session", "e2e-safe-call"
	response := h.request(scenario, "x-gram-session-id", sessionID, callID, "gram-e2e", "safe prompt", false)
	require.Equal(h.t, http.StatusOK, response.Status, string(response.Body))
	h.requireCompletion(response.Body)
	require.Equal(h.t, 1, h.provider.count(h.key(scenario, sessionID, callID)))
	h.requireConversation(h.messages(sessionID, 2), h.prompt(scenario, sessionID, callID, "safe prompt"))
}

func (h *proxyHarness) nativeSessionHeaders() {
	for _, header := range []string{"x-claude-code-session-id", "session-id", "thread-id", "x-session-id"} {
		scenario := "native-" + header
		sessionID := "e2e-" + header + "-session"
		for turn := 1; turn <= 2; turn++ {
			callID := fmt.Sprintf("e2e-%s-call-%d", header, turn)
			text := fmt.Sprintf("turn %d", turn)
			response := h.request(scenario, header, sessionID, callID, "gram-e2e", text, false)
			require.Equal(h.t, http.StatusOK, response.Status, string(response.Body))
			h.requireCompletion(response.Body)
			require.Equal(h.t, 1, h.provider.count(h.key(scenario, sessionID, callID)))
		}

		messages := h.messages(sessionID, 4)
		require.Equal(h.t, []string{"user", "assistant", "user", "assistant"}, []string{
			messages[0].Role,
			messages[1].Role,
			messages[2].Role,
			messages[3].Role,
		})
		require.Equal(h.t, h.prompt(scenario, sessionID, "e2e-"+header+"-call-1", "turn 1"), messages[0].Content)
		require.Equal(h.t, fixtureAnswer, messages[1].Content)
		require.Equal(h.t, h.prompt(scenario, sessionID, "e2e-"+header+"-call-2", "turn 2"), messages[2].Content)
		require.Equal(h.t, fixtureAnswer, messages[3].Content)
	}

	scenario, sessionID, callID := "session-header-precedence", "e2e-gram-session", "e2e-precedence-call"
	response := h.request(scenario, "x-gram-session-id", sessionID, callID, "gram-e2e", "precedence prompt", false, map[string]string{
		"x-claude-code-session-id": "e2e-claude-session",
		"session-id":               "e2e-codex-session",
		"thread-id":                "e2e-codex-thread",
		"x-session-id":             "e2e-opencode-session",
	})
	require.Equal(h.t, http.StatusOK, response.Status, string(response.Body))
	h.requireCompletion(response.Body)
	require.Equal(h.t, 1, h.provider.count(h.key(scenario, sessionID, callID)))
	h.requireConversation(h.messages(sessionID, 2), h.prompt(scenario, sessionID, callID, "precedence prompt"))
}

func (h *proxyHarness) blockedNonStreaming() {
	scenario, sessionID, callID := "blocked", "e2e-blocked-session", "e2e-blocked-call"
	response := h.request(scenario, "x-gram-session-id", sessionID, callID, "gram-e2e", "token="+syntheticSecret, false)
	require.Equal(h.t, http.StatusBadRequest, response.Status, string(response.Body))
	h.requireBlocked(response.Body)
	require.Zero(h.t, h.provider.count(h.key(scenario, sessionID, callID)))
	messages := h.messages(sessionID, 1)
	require.Equal(h.t, "user", messages[0].Role)
	require.Equal(h.t, h.prompt(scenario, sessionID, callID, "token="+syntheticSecret), messages[0].Content)
	require.Equal(h.t, "litellm", messages[0].Source.String)
	h.materializeFinding(messages[0].ID)
}

func (h *proxyHarness) aiAccessHeadersBlockBeforeProvider() {
	activation, err := h.lifecycle.ActivatePrescription(h.ctx, killswitches.ActivatePrescriptionRequest{
		MutationContext: killswitches.MutationContext{
			OrganizationID: killswitches.OrganizationID(h.orgID), ActorUserID: h.userID, ActorDisplayName: h.userID, OperationID: uuid.New(),
		},
		Definition: mcptoolexecution.DefinitionKeyAIAccess, PrincipalKind: mcptoolexecution.PrincipalKindUser, PrincipalInput: h.userID,
		ResourceKind: mcptoolexecution.ResourceKindLiteLLMInstance,
		Desired: killswitches.DesiredVersionInput{
			ResourceScope: killswitches.ResourceScopeSelected, SelectedResourceInputs: []string{h.instanceID}, StartMode: killswitches.StartModeNow,
			InternalNote: "LiteLLM proxy E2E", ExternalNote: "AI access selected note.",
		},
	})
	require.NoError(h.t, err)
	defer func() {
		_, deactivateErr := h.lifecycle.DeactivatePrescription(h.ctx, killswitches.DeactivatePrescriptionRequest{
			MutationContext: killswitches.MutationContext{
				OrganizationID: killswitches.OrganizationID(h.orgID), ActorUserID: h.userID, ActorDisplayName: h.userID, OperationID: uuid.New(),
			},
			PrescriptionID: activation.PrescriptionID, ExpectedVersion: activation.Version,
		})
		assert.NoError(h.t, deactivateErr)
	}()

	scenario, sessionID, callID := "ai-access", "e2e-ai-access-session", "e2e-ai-access-call"
	response := h.request(scenario, "x-gram-session-id", sessionID, callID, "gram-e2e", "safe prompt", false)
	require.GreaterOrEqual(h.t, response.Status, http.StatusBadRequest, string(response.Body))
	require.Equal(h.t, "AI access selected note.", blockedMessage(h.t, response.Body))
	require.Zero(h.t, h.provider.count(h.key(scenario, sessionID, callID)))
	h.noMessages(sessionID)

	invocationID, err := uuid.NewV7()
	require.NoError(h.t, err)
	crossTenantAssertion, err := h.signer.MintAssertion(h.userID, litellmacting.AssertionBinding{
		OrganizationID: "org_other", ProjectID: h.projectID.String(), InstanceID: h.instanceID, APIKeyID: h.apiKeyID, InvocationID: invocationID.String(),
	})
	require.NoError(h.t, err)
	crossScenario, crossSession, crossCall := "ai-access-cross-tenant", "e2e-ai-access-cross-tenant-session", "e2e-ai-access-cross-tenant-call"
	crossTenant := h.requestWithOptions(proxyRequestOptions{
		scenario: crossScenario, sessionHeader: "x-gram-session-id", sessionID: crossSession, callID: crossCall, guardrail: "gram-e2e", text: "safe prompt",
		actingHeaders: map[string]string{
			actingPrincipalHeader: crossTenantAssertion, actingPrincipalContractHeader: litellmacting.ContractVersion, inferenceInvocationHeader: invocationID.String(),
		},
	})
	require.GreaterOrEqual(h.t, crossTenant.Status, http.StatusBadRequest, string(crossTenant.Body))
	require.Equal(h.t, liteLLMIdentityFailureMessage, blockedMessage(h.t, crossTenant.Body))
	require.Zero(h.t, h.provider.count(h.key(crossScenario, crossSession, crossCall)))
	h.noMessages(crossSession)

	missingScenario, missingSession, missingCall := "ai-access-missing", "e2e-ai-access-missing-session", "e2e-ai-access-missing-call"
	missing := h.requestWithOptions(proxyRequestOptions{
		scenario: missingScenario, sessionHeader: "x-gram-session-id", sessionID: missingSession, callID: missingCall, guardrail: "gram-e2e", text: "safe prompt",
	})
	require.GreaterOrEqual(h.t, missing.Status, http.StatusBadRequest, string(missing.Body))
	require.Equal(h.t, liteLLMIdentityFailureMessage, blockedMessage(h.t, missing.Body))
	require.Zero(h.t, h.provider.count(h.key(missingScenario, missingSession, missingCall)))
	h.noMessages(missingSession)
}

func (h *proxyHarness) safeStreaming() {
	scenario, sessionID, callID := "stream-safe", "e2e-stream-safe-session", "e2e-stream-safe-call"
	response := h.request(scenario, "x-gram-session-id", sessionID, callID, "gram-e2e", "safe streaming prompt", true)
	require.Equal(h.t, http.StatusOK, response.Status, string(response.Body))
	require.Contains(h.t, response.Header.Get("Content-Type"), "text/event-stream")
	h.requireSSE(response.Body)
	require.Equal(h.t, 1, h.provider.count(h.key(scenario, sessionID, callID)))
	h.requireConversation(h.messages(sessionID, 2), h.prompt(scenario, sessionID, callID, "safe streaming prompt"))
}

func (h *proxyHarness) blockedStreaming() {
	scenario, sessionID, callID := "stream-blocked", "e2e-stream-blocked-session", "e2e-stream-blocked-call"
	response := h.request(scenario, "x-gram-session-id", sessionID, callID, "gram-e2e", "token="+syntheticSecret, true)
	require.Equal(h.t, http.StatusBadRequest, response.Status, string(response.Body))
	h.requireBlocked(response.Body)
	require.Zero(h.t, h.provider.count(h.key(scenario, sessionID, callID)))
	messages := h.messages(sessionID, 1)
	require.Equal(h.t, "user", messages[0].Role)
	require.Equal(h.t, h.prompt(scenario, sessionID, callID, "token="+syntheticSecret), messages[0].Content)
	require.Equal(h.t, "litellm", messages[0].Source.String)
}

func (h *proxyHarness) failClosed() {
	scenario, sessionID, callID := "fail-closed", "e2e-fail-closed-session", "e2e-fail-closed-call"
	response := h.request(scenario, "x-gram-session-id", sessionID, callID, "gram-e2e-fail-closed", "outage prompt", false)
	require.NotContains(h.t, []int{http.StatusOK, http.StatusCreated, http.StatusNoContent}, response.Status, string(response.Body))
	require.Zero(h.t, h.provider.count(h.key(scenario, sessionID, callID)))
	h.noMessages(sessionID)
}

func (h *proxyHarness) failOpen() {
	scenario, sessionID, callID := "fail-open", "e2e-fail-open-session", "e2e-fail-open-call"
	response := h.request(scenario, "x-gram-session-id", sessionID, callID, "gram-e2e-fail-open", "outage prompt", false)
	require.Equal(h.t, http.StatusOK, response.Status, string(response.Body))
	require.Equal(h.t, 1, h.provider.count(h.key(scenario, sessionID, callID)))
	h.noMessages(sessionID)
}

func (h *proxyHarness) timeoutAndResend() {
	// LiteLLM 1.94.0 does not retry a timed-out guardrail callback. A gateway may
	// safely resend with the same call ID; an ordinary retry with a new call ID is distinct.
	scenario, sessionID, callID := "timeout", "e2e-timeout-session", "e2e-timeout-call"
	first := h.request(scenario, "x-gram-session-id", sessionID, callID, "gram-e2e-timeout", "timeout prompt", false)
	require.NotEqual(h.t, http.StatusOK, first.Status, string(first.Body))
	require.Zero(h.t, h.provider.count(h.key(scenario, sessionID, callID)))
	require.Equal(h.t, "user", h.messages(sessionID, 1)[0].Role)
	h.relay.releaseFirst()
	require.EventuallyWithT(h.t, func(collect *assert.CollectT) {
		assert.False(collect, h.relay.isWaiting())
	}, 3*time.Second, 10*time.Millisecond)
	require.Never(h.t, func() bool {
		return h.provider.count(h.key(scenario, sessionID, callID)) > 0
	}, 3*time.Second, 10*time.Millisecond)
	second := h.request(scenario, "x-gram-session-id", sessionID, callID, "gram-e2e-timeout", "timeout prompt", false)
	require.Equal(h.t, http.StatusOK, second.Status, string(second.Body))
	require.Equal(h.t, 1, h.provider.count(h.key(scenario, sessionID, callID)))
	h.requireConversation(h.messages(sessionID, 2), h.prompt(scenario, sessionID, callID, "timeout prompt"))
}

func (h *proxyHarness) materializeFinding(messageID uuid.UUID) {
	h.t.Helper()
	customRules, err := customruleanalyzer.NewScanner(h.conn)
	require.NoError(h.t, err)
	celEngine, err := riskcelenv.New()
	require.NoError(h.t, err)
	flags := &feature.InMemory{}
	shadowMCPClient := shadowmcp.NewClient(testenv.NewLogger(h.t), h.conn, cache.NoopCache, nil)
	analyze, err := riskanalysis.NewAnalyzeBatch(
		testenv.NewLogger(h.t), testenv.NewTracerProvider(h.t), testenv.NewMeterProvider(h.t), h.conn,
		nil, &riskanalysis.StubPIIScanner{}, nil, shadowMCPClient, noMCPProvenance{}, nil, flags,
		gcp.NewNoopPublisher[*riskv1.PresidioAnalysis](),
		gcp.NewNoopPublisher[*riskv1.GitleaksAnalysis](),
		gcp.NewNoopPublisher[*riskv1.PromptInjectionAnalysis](),
		gcp.NewNoopPublisher[*riskv1.PromptPolicyAnalysis](),
		gcp.NewNoopPublisher[*riskv1.CustomRulesAnalysis](),
		gcp.NewNoopPublisher[*riskv1.Finding](),
		customRules, celEngine, nil, nil,
	)
	require.NoError(h.t, err)
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestActivityEnvironment()
	env.RegisterActivity(analyze.Do)
	activityResult, err := env.ExecuteActivity(analyze.Do, riskanalysis.AnalyzeBatchArgs{
		ProjectID: h.projectID, OrganizationID: h.orgID, RiskPolicyID: h.policy.ID, PolicyVersion: h.policy.Version,
		MessageIDs: []uuid.UUID{messageID}, ContentPartIDs: nil, Sources: h.policy.Sources, MessageTypes: h.policy.MessageTypes,
		PresidioEntities: nil, PresidioScoreThreshold: 0, CustomRuleIds: nil, ApprovedEmailDomains: nil,
		BuiltinPresetsEnabled: false, DetectionScopes: nil,
	})
	require.NoError(h.t, err)
	var analyzed riskanalysis.AnalyzeBatchResult
	require.NoError(h.t, activityResult.Get(&analyzed))
	require.Equal(h.t, 1, analyzed.Processed)
	require.Positive(h.t, analyzed.Findings)
	findings, err := riskrepo.New(h.conn).ListRiskResultsByProjectAndPolicy(h.t.Context(), riskrepo.ListRiskResultsByProjectAndPolicyParams{
		ProjectID: h.projectID, RiskPolicyID: h.policy.ID, CursorMessageCreatedAt: pgtype.Timestamptz{},
		CursorID: uuid.NullUUID{}, PageLimit: 10,
	})
	require.NoError(h.t, err)
	require.NotEmpty(h.t, findings)
	require.Equal(h.t, riskanalysis.SourceGitleaks, findings[0].Source)
	require.True(h.t, findings[0].Found)
	require.Equal(h.t, messageID, findings[0].ChatMessageID.UUID)
}
