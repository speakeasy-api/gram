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
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/typesafe"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

const testOrgID = "org_test"

// fakeKeys stands in for the OpenRouter provisioner. It satisfies both the
// read-only lookup the launcher depends on and ProvisionAPIKey, so tests can
// prove the launcher never reaches for the provisioning path even when the
// value handed to it would allow it.
type fakeKeys struct {
	key string
	ok  bool
	err error

	mu             sync.Mutex
	lookups        []keyCall
	provisionCalls int
}

type keyCall struct {
	orgID   string
	keyType openrouter.KeyType
}

func (f *fakeKeys) LookupAPIKey(_ context.Context, orgID string, keyType openrouter.KeyType) (string, bool, error) {
	f.mu.Lock()
	f.lookups = append(f.lookups, keyCall{orgID: orgID, keyType: keyType})
	f.mu.Unlock()
	if f.err != nil {
		return "", false, f.err
	}
	return f.key, f.ok, nil
}

func (f *fakeKeys) ProvisionAPIKey(_ context.Context, _ string, _ openrouter.KeyType) (string, error) {
	f.mu.Lock()
	f.provisionCalls++
	f.mu.Unlock()
	return "sk-or-minted", nil
}

func newFakeKeys(key string) *fakeKeys {
	return &fakeKeys{key: key, ok: key != "", err: nil, mu: sync.Mutex{}, lookups: nil, provisionCalls: 0}
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

func newTestService(t *testing.T, jev *fakeJev, keys keyLookup) *Service {
	t.Helper()
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	client := typesafe.NewClient(policy.Client(), testenv.NewLogger(t), typesafe.WithEndpoint(jev.server.URL))
	return &Service{
		tracer:  testenv.NewTracerProvider(t).Tracer("test"),
		logger:  testenv.NewLogger(t),
		db:      nil,
		auth:    nil,
		authz:   authz.NewEngine(testenv.NewLogger(t), nil, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient()),
		keys:    keys,
		client:  client,
		metrics: newMetrics(testenv.NewLogger(t), testenv.NewMeterProvider(t)),
	}
}

// authedContext is a session-authenticated member holding exactly the org
// read scope the launcher requires. A session id is set so RBAC is enforced
// rather than skipped as an internal call.
func authedContext(t *testing.T) context.Context {
	t.Helper()
	return authztest.WithExactGrants(t, sessionContext(t), authz.NewGrant(authz.ScopeOrgRead, testOrgID))
}

func sessionContext(t *testing.T) context.Context {
	t.Helper()
	return contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID:  testOrgID,
		UserID:                "user_test",
		ExternalUserID:        "",
		APIKeyID:              "",
		APIKeyName:            "",
		OrgWidePluginHooksKey: false,
		SessionID:             conv.PtrEmpty("sess_test"),
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
	keys := newFakeKeys("sk-or-org-key")
	svc := newTestService(t, jev, keys)

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

	// The design promises every field is present whenever disabled is false.
	require.NotNil(t, res.Target)
	require.NotNil(t, res.Action)
	require.NotNil(t, res.Ready)
	require.InDelta(t, 0.82, *res.Ready, 1e-9)
	require.NotNil(t, res.LatencyMs)
	require.Less(t, *res.LatencyMs, typesafe.DefaultTimeout.Milliseconds(), "a successful round trip completes inside the client timeout")

	require.Equal(t, int32(1), jev.requests.Load())
	jev.mu.Lock()
	defer jev.mu.Unlock()
	require.Equal(t, "Bearer sk-or-org-key", jev.lastAuth)
	require.Equal(t, "typesafe/jev-1.13", jev.lastModel)
	require.Len(t, jev.lastTarget, 4)

	keys.mu.Lock()
	defer keys.mu.Unlock()
	require.Equal(t, []keyCall{{orgID: testOrgID, keyType: openrouter.KeyTypeInternal}}, keys.lookups)
	require.Zero(t, keys.provisionCalls, "the launcher only reads existing keys")
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
	svc := newTestService(t, jev, newFakeKeys("k"))

	res, err := svc.Judge(authedContext(t), fixturePayload())
	require.NoError(t, err)
	require.False(t, res.Disabled)
	require.InDelta(t, 0.8, res.Target["page:/settings"], 1e-9)
	require.Equal(t, map[string]float64{"unclear": 1}, res.Action)
	require.NotNil(t, res.Ready)
	require.InDelta(t, 0, *res.Ready, 1e-9)
	require.NotNil(t, res.LatencyMs, "latency is reported even when the judge answers partially")
}

func TestJudgeTransportFailure(t *testing.T) {
	t.Parallel()

	jev := newFakeJev(t, http.StatusOK, happyBody)
	svc := newTestService(t, jev, newFakeKeys("k"))
	// Closing the listener leaves the client pointed at a dead port, the
	// shape of an OpenRouter outage or a network partition.
	jev.server.Close()

	res, err := svc.Judge(authedContext(t), fixturePayload())
	require.Nil(t, res)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeGatewayError, oopsErr.Code)
	require.ErrorIs(t, err, typesafe.ErrTransport)
	require.Equal(t, int32(0), jev.requests.Load())
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
	svc := newTestService(t, jev, newFakeKeys("k"))

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

	cases := map[string]*fakeKeys{
		"no key provisioned": newFakeKeys(""),
		"empty key row":      {key: "", ok: true, err: nil, mu: sync.Mutex{}, lookups: nil, provisionCalls: 0},
		"unset placeholder":  newFakeKeys("unset"),
	}
	for name, keys := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			jev := newFakeJev(t, http.StatusOK, happyBody)
			svc := newTestService(t, jev, keys)

			res, err := svc.Judge(authedContext(t), fixturePayload())
			require.NoError(t, err)
			require.True(t, res.Disabled)
			require.Nil(t, res.Target)
			require.Nil(t, res.Action)
			require.Nil(t, res.Ready)
			require.Nil(t, res.LatencyMs)
			require.Equal(t, int32(0), jev.requests.Load(), "no outbound call without a usable key")

			keys.mu.Lock()
			defer keys.mu.Unlock()
			require.Zero(t, keys.provisionCalls, "an organization without a key must not have one minted by the palette")
		})
	}
}

// TestJudgeKeyLookupErrorIsGatewayError pins that a key which exists but
// cannot be read right now (DB blip, admin lock) surfaces as a retryable
// gateway error rather than disabled, which the palette would latch on.
func TestJudgeKeyLookupErrorIsGatewayError(t *testing.T) {
	t.Parallel()

	jev := newFakeJev(t, http.StatusOK, happyBody)
	keys := &fakeKeys{key: "", ok: false, err: errors.New("key disabled"), mu: sync.Mutex{}, lookups: nil, provisionCalls: 0}
	svc := newTestService(t, jev, keys)

	res, err := svc.Judge(authedContext(t), fixturePayload())
	require.Nil(t, res)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeGatewayError, oopsErr.Code)
	require.Equal(t, int32(0), jev.requests.Load())
}

func TestJudgeRequiresOrgRead(t *testing.T) {
	t.Parallel()

	jev := newFakeJev(t, http.StatusOK, happyBody)
	keys := newFakeKeys("k")
	svc := newTestService(t, jev, keys)

	res, err := svc.Judge(authztest.WithExactGrants(t, sessionContext(t)), fixturePayload())
	require.Nil(t, res)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
	require.Equal(t, int32(0), jev.requests.Load(), "no key is spent for a caller without the scope")

	keys.mu.Lock()
	defer keys.mu.Unlock()
	require.Empty(t, keys.lookups, "the key is not even read for a caller without the scope")
}

func TestJudgeRejectsReservedAndDuplicateIDs(t *testing.T) {
	t.Parallel()

	for name, ids := range map[string][]string{
		"reserved none": {"page:/settings", "none"},
		"duplicate":     {"page:/settings", "page:/settings"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			jev := newFakeJev(t, http.StatusOK, happyBody)
			svc := newTestService(t, jev, newFakeKeys("k"))

			payload := fixturePayload()
			payload.Candidates = nil
			for _, id := range ids {
				payload.Candidates = append(payload.Candidates, &gen.LauncherCandidate{ID: id, Kind: "page", Title: "P", Detail: nil, Verbs: nil})
			}

			res, err := svc.Judge(authedContext(t), payload)
			require.Nil(t, res)
			var oopsErr *oops.ShareableError
			require.ErrorAs(t, err, &oopsErr)
			require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
			require.Equal(t, int32(0), jev.requests.Load())
		})
	}
}

func TestJudgeNoRankableCandidatesSkipsUpstream(t *testing.T) {
	t.Parallel()

	for name, cands := range map[string][]*gen.LauncherCandidate{
		"empty list": {},
		"only people": {
			{ID: "person:1", Kind: "person", Title: "Ada Lovelace", Detail: conv.PtrEmpty("Member · admin"), Verbs: []string{"open"}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			jev := newFakeJev(t, http.StatusOK, happyBody)
			svc := newTestService(t, jev, newFakeKeys("k"))

			payload := fixturePayload()
			payload.Candidates = cands

			res, err := svc.Judge(authedContext(t), payload)
			require.NoError(t, err)
			require.False(t, res.Disabled)
			require.Equal(t, map[string]float64{"none": 1}, res.Target)
			require.Equal(t, map[string]float64{"unclear": 1}, res.Action)
			require.NotNil(t, res.Ready)
			require.InDelta(t, 0, *res.Ready, 1e-9)
			require.NotNil(t, res.LatencyMs)
			require.Zero(t, *res.LatencyMs)
			require.Equal(t, int32(0), jev.requests.Load(), "nothing to rank means nothing to ask")
		})
	}
}

func TestJudgeRejectsNullCandidate(t *testing.T) {
	t.Parallel()

	jev := newFakeJev(t, http.StatusOK, happyBody)
	svc := newTestService(t, jev, newFakeKeys("k"))

	payload := fixturePayload()
	payload.Candidates = append(payload.Candidates, nil)

	res, err := svc.Judge(authedContext(t), payload)
	require.Nil(t, res)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
	require.Equal(t, int32(0), jev.requests.Load())
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
			svc := newTestService(t, jev, newFakeKeys("k"))

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
	svc := newTestService(t, jev, newFakeKeys("k"))

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
