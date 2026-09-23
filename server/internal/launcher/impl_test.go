package launcher

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/launcher"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/typesafe"
)

type fakeProvisioner struct {
	key string
	err error

	mu    sync.Mutex
	calls []provisionCall
}

type provisionCall struct {
	orgID   string
	keyType openrouter.KeyType
}

func (f *fakeProvisioner) ProvisionAPIKey(_ context.Context, orgID string, keyType openrouter.KeyType) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, provisionCall{orgID: orgID, keyType: keyType})
	f.mu.Unlock()
	if f.err != nil {
		return "", f.err
	}
	return f.key, nil
}

// fakeJev is an httptest stand-in for the OpenRouter-hosted System One
// endpoint. Each request is recorded so tests can assert on headers and body.
type fakeJev struct {
	server *httptest.Server

	requests atomic.Int32
	status   int
	body     string

	mu         sync.Mutex
	lastAuth   string
	lastModel  string
	lastState  json.RawMessage
	lastTarget map[string]string
}

func newFakeJev(t *testing.T, status int, body string) *fakeJev {
	t.Helper()
	f := &fakeJev{status: status, body: body}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		var req typesafe.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		f.lastAuth = r.Header.Get("Authorization")
		f.lastModel = req.Model
		f.lastState = req.State
		f.lastTarget = req.Questions[questionTarget].Criteria
		f.mu.Unlock()
		w.WriteHeader(f.status)
		_, _ = io.WriteString(w, f.body)
	}))
	t.Cleanup(f.server.Close)
	return f
}

const happyBody = `{
	"id": "gen-01",
	"provider": "TypeSafe",
	"model": "typesafe/jev-2026-01",
	"answers": {
		"target": {"type": "choice", "choice": "c0", "probabilities": {"c0": 0.7, "c1": 0.2, "c2": 0.05, "none": 0.05}},
		"action": {"type": "choice", "choice": "enable", "probabilities": {"open": 0.2, "enable": 0.7, "disable": 0.05, "publish": 0.0, "unclear": 0.05}},
		"ready": {"type": "noul", "noul": 0.82}
	},
	"usage": {"input_tokens": 300, "output_tokens": 9, "cost": 0.0004}
}`

func newTestService(t *testing.T, jev *fakeJev, provisioner keyProvisioner) *Service {
	t.Helper()
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	client := typesafe.NewClient(policy.Client(), testenv.NewLogger(t), typesafe.WithEndpoint(jev.server.URL))
	return &Service{
		tracer:      testenv.NewTracerProvider(t).Tracer("test"),
		logger:      testenv.NewLogger(t),
		db:          nil,
		auth:        nil,
		provisioner: provisioner,
		client:      client,
		metrics:     newMetrics(testenv.NewLogger(t), testenv.NewMeterProvider(t)),
	}
}

func authedContext(t *testing.T) context.Context {
	t.Helper()
	return contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID:  "org_test",
		UserID:                "user_test",
		ExternalUserID:        "",
		APIKeyID:              "",
		APIKeyName:            "",
		OrgWidePluginHooksKey: false,
		SessionID:             nil,
		ProjectID:             nil,
		OrganizationSlug:      "test",
		Email:                 nil,
		AccountType:           "free",
		HasActiveSubscription: false,
	})
}

func fixturePayload() *gen.JudgePayload {
	return &gen.JudgePayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Query:            "the disabled slack one",
		Context:          &gen.LauncherContext{Route: conv.PtrEmpty("/mcp")},
		Candidates: []*gen.LauncherCandidate{
			{ID: "mcp:slack", Kind: "mcp_server", Title: "Slack", Detail: conv.PtrEmpty("MCP server · disabled"), Verbs: []string{"open", "enable"}},
			{ID: "page:/settings", Kind: "page", Title: "Settings", Detail: conv.PtrEmpty("Page"), Verbs: []string{"open"}},
			{ID: "marketplace", Kind: "marketplace", Title: "Plugin marketplace", Detail: conv.PtrEmpty("Plugin marketplace · unpublished changes"), Verbs: []string{"open", "publish"}},
		},
	}
}

func TestJudgeMapsAnswersToCallerIDs(t *testing.T) {
	t.Parallel()

	jev := newFakeJev(t, http.StatusOK, happyBody)
	prov := &fakeProvisioner{key: "sk-or-org-key", err: nil, mu: sync.Mutex{}, calls: nil}
	svc := newTestService(t, jev, prov)

	res, err := svc.Judge(authedContext(t), fixturePayload())
	require.NoError(t, err)
	require.False(t, res.Disabled)

	require.InDelta(t, 0.7, res.Target["mcp:slack"], 1e-9)
	require.InDelta(t, 0.2, res.Target["page:/settings"], 1e-9)
	require.InDelta(t, 0.05, res.Target["marketplace"], 1e-9)
	require.InDelta(t, 0.05, res.Target["none"], 1e-9)
	require.NotContains(t, res.Target, "c0", "index ids must not leak to the caller")

	require.InDelta(t, 0.7, res.Action["enable"], 1e-9)
	require.InDelta(t, 0.2, res.Action["open"], 1e-9)
	require.Len(t, res.Action, 5)

	require.NotNil(t, res.Ready)
	require.InDelta(t, 0.82, *res.Ready, 1e-9)
	require.NotNil(t, res.LatencyMs)
	require.GreaterOrEqual(t, *res.LatencyMs, int64(0))

	require.Equal(t, int32(1), jev.requests.Load())
	jev.mu.Lock()
	defer jev.mu.Unlock()
	require.Equal(t, "Bearer sk-or-org-key", jev.lastAuth)
	require.Equal(t, "typesafe/jev-latest", jev.lastModel)
	require.Len(t, jev.lastTarget, 4)

	prov.mu.Lock()
	defer prov.mu.Unlock()
	require.Equal(t, []provisionCall{{orgID: "org_test", keyType: openrouter.KeyTypeInternal}}, prov.calls)
}

func TestJudgeMissingAnswersFallBack(t *testing.T) {
	t.Parallel()

	jev := newFakeJev(t, http.StatusOK, `{
		"model": "typesafe/jev-2026-01",
		"answers": {
			"target": {"type": "choice", "choice": "c1", "probabilities": {"c0": 0.1, "c1": 0.8, "c2": 0.1, "none": 0.0}}
		},
		"usage": {"input_tokens": 10, "output_tokens": 1}
	}`)
	svc := newTestService(t, jev, &fakeProvisioner{key: "k", err: nil, mu: sync.Mutex{}, calls: nil})

	res, err := svc.Judge(authedContext(t), fixturePayload())
	require.NoError(t, err)
	require.False(t, res.Disabled)
	require.InDelta(t, 0.8, res.Target["page:/settings"], 1e-9)
	require.Equal(t, map[string]float64{"unclear": 1}, res.Action)
	require.NotNil(t, res.Ready)
	require.InDelta(t, 0, *res.Ready, 1e-9)
}

func TestJudgeDropsPersonCandidates(t *testing.T) {
	t.Parallel()

	jev := newFakeJev(t, http.StatusOK, `{
		"model": "typesafe/jev-2026-01",
		"answers": {
			"target": {"type": "choice", "choice": "c0", "probabilities": {"c0": 0.9, "none": 0.1}},
			"action": {"type": "choice", "choice": "open", "probabilities": {"open": 1}},
			"ready": {"type": "noul", "noul": 0.9}
		},
		"usage": {"input_tokens": 10, "output_tokens": 1}
	}`)
	svc := newTestService(t, jev, &fakeProvisioner{key: "k", err: nil, mu: sync.Mutex{}, calls: nil})

	payload := fixturePayload()
	payload.Candidates = []*gen.LauncherCandidate{
		{ID: "person:1", Kind: "person", Title: "Ada Lovelace", Detail: conv.PtrEmpty("Member · admin"), Verbs: []string{"open"}},
		{ID: "page:/settings", Kind: "page", Title: "Settings", Detail: conv.PtrEmpty("Page"), Verbs: []string{"open"}},
	}

	res, err := svc.Judge(authedContext(t), payload)
	require.NoError(t, err)
	require.InDelta(t, 0.9, res.Target["page:/settings"], 1e-9)
	require.NotContains(t, res.Target, "person:1")

	jev.mu.Lock()
	defer jev.mu.Unlock()
	require.NotContains(t, string(jev.lastState), "Lovelace")
	require.Len(t, jev.lastTarget, 2)
}

func TestJudgeDisabledWithoutKey(t *testing.T) {
	t.Parallel()

	cases := map[string]*fakeProvisioner{
		"empty key":         {key: "", err: nil, mu: sync.Mutex{}, calls: nil},
		"unset placeholder": {key: "unset", err: nil, mu: sync.Mutex{}, calls: nil},
		"provisioner error": {key: "", err: errors.New("no key for org"), mu: sync.Mutex{}, calls: nil},
	}
	for name, prov := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			jev := newFakeJev(t, http.StatusOK, happyBody)
			svc := newTestService(t, jev, prov)

			res, err := svc.Judge(authedContext(t), fixturePayload())
			require.NoError(t, err)
			require.True(t, res.Disabled)
			require.Nil(t, res.Target)
			require.Nil(t, res.Action)
			require.Nil(t, res.Ready)
			require.Nil(t, res.LatencyMs)
			require.Equal(t, int32(0), jev.requests.Load(), "no outbound call without a usable key")
		})
	}
}

func TestJudgeUpstreamFailure(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		status int
		body   string
	}{
		"unauthorized": {status: http.StatusUnauthorized, body: `{"error":"bad key"}`},
		"server error": {status: http.StatusInternalServerError, body: `oops`},
		"malformed":    {status: http.StatusOK, body: `{"answers": {`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			jev := newFakeJev(t, tc.status, tc.body)
			svc := newTestService(t, jev, &fakeProvisioner{key: "k", err: nil, mu: sync.Mutex{}, calls: nil})

			res, err := svc.Judge(authedContext(t), fixturePayload())
			require.Nil(t, res)
			var oopsErr *oops.ShareableError
			require.ErrorAs(t, err, &oopsErr)
			require.Equal(t, oops.CodeGatewayError, oopsErr.Code)
			require.Equal(t, int32(1), jev.requests.Load())
		})
	}
}

func TestJudgeRejectsTooManyCandidates(t *testing.T) {
	t.Parallel()

	jev := newFakeJev(t, http.StatusOK, happyBody)
	svc := newTestService(t, jev, &fakeProvisioner{key: "k", err: nil, mu: sync.Mutex{}, calls: nil})

	payload := fixturePayload()
	payload.Candidates = nil
	for i := range maxCandidates + 1 {
		payload.Candidates = append(payload.Candidates, &gen.LauncherCandidate{
			ID: "page:" + string(rune('a'+i%26)), Kind: "page", Title: "P", Detail: nil, Verbs: nil,
		})
	}

	res, err := svc.Judge(authedContext(t), payload)
	require.Nil(t, res)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
	require.Equal(t, int32(0), jev.requests.Load())
}
