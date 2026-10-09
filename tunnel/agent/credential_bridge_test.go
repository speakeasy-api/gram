//go:build unix

package agent

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/tunnel/identity"
)

type credentialTestServer struct {
	srv    *httptest.Server
	bridge *stdioBridge
	signer *testSigner
	store  *fakeCredentialStore
	clock  *testClock
	logs   *syncBuffer
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type credentialServerOptions struct {
	command     string
	env         []string
	maxAge      time.Duration
	expiryGrace time.Duration
}

func newCredentialTestServer(t *testing.T, opts credentialServerOptions) *credentialTestServer {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	if opts.command == "" {
		// exec, as documented, so the server owns its stdin pipe alone.
		opts.command = "exec env " + stdioFixtureEnv + "=1 " + stdioFixtureReadTokenAtStart + "=1 '" + exe + "'"
	}
	if opts.maxAge == 0 {
		opts.maxAge = time.Hour
	}
	if opts.expiryGrace == 0 {
		opts.expiryGrace = credentialExpiryGrace
	}
	signer := newTestSigner(t)
	clock := newTestClock()
	store := newFakeCredentialStore(t)
	logs := &syncBuffer{}
	logger := slog.New(slog.NewTextHandler(logs, nil))
	broker := &credentialBroker{
		verifier:    newAssertionVerifier(signer.config(), jwksHTTPClient(), clock.Now),
		store:       store,
		maxAge:      opts.maxAge,
		expiryGrace: opts.expiryGrace,
		now:         clock.Now,
		cleanups:    sync.WaitGroup{},
	}
	b := newStdioBridge(opts.command, 0, 0, broker, logger)
	if opts.env != nil {
		b.env = opts.env
	}
	srv := httptest.NewServer(b)
	t.Cleanup(func() {
		srv.Close()
		b.Close()
	})
	return &credentialTestServer{srv: srv, bridge: b, signer: signer, store: store, clock: clock, logs: logs}
}

type credentialCall struct {
	method    string
	sid       string
	body      string
	principal testPrincipal
	grant     testGrant
	// token is the bearer sent; vouched is the token the assertion vouches
	// for. Empty vouched means the bearer itself.
	token     string
	vouched   string
	expiresAt time.Time
	// noCredential omits the upstream_credential claim.
	noCredential bool
	// rawCredential replaces the claim verbatim.
	rawCredential any
	// noAssertion omits the assertion; badSignature corrupts it.
	noAssertion  bool
	badSignature bool
}

func (c *credentialTestServer) do(t *testing.T, call credentialCall) *http.Response {
	t.Helper()
	if call.method == "" {
		call.method = http.MethodPost
	}
	if call.principal == (testPrincipal{}) {
		call.principal = defaultTestPrincipal
	}
	if call.grant == (testGrant{}) {
		call.grant = defaultTestGrant
	}
	var reader io.Reader
	if call.body != "" {
		reader = strings.NewReader(call.body)
	}
	req, err := http.NewRequestWithContext(t.Context(), call.method, c.srv.URL+"/", reader)
	require.NoError(t, err)
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	if call.sid != "" {
		req.Header.Set(headerMCPSessionID, call.sid)
	}
	if call.token != "" {
		req.Header.Set("Authorization", "Bearer "+call.token)
	}
	if !call.noAssertion {
		vouched := call.vouched
		if vouched == "" {
			vouched = call.token
		}
		var credential map[string]any
		if !call.noCredential && vouched != "" {
			credential = credentialClaim(call.grant, vouched, call.expiresAt)
		}
		claims := c.signer.claims(c.clock.Now(), call.principal, credential)
		if call.rawCredential != nil {
			claims["upstream_credential"] = call.rawCredential
		}
		raw := c.signer.sign(t, claims)
		if call.badSignature {
			raw = raw[:len(raw)-4] + "AAAA"
		}
		req.Header.Set(identity.Header, raw)
	}
	resp, err := c.srv.Client().Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

const initializeBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`

func (c *credentialTestServer) initialize(t *testing.T, call credentialCall) string {
	t.Helper()
	call.body = initializeBody
	resp := c.do(t, call)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	sid := resp.Header.Get(headerMCPSessionID)
	require.NotEmpty(t, sid)
	call.sid = sid
	call.body = `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	require.Equal(t, http.StatusAccepted, c.do(t, call).StatusCode)
	return sid
}

// toolText calls a fixture tool and returns its text result.
func (c *credentialTestServer) toolText(t *testing.T, call credentialCall, tool string) string {
	t.Helper()
	call.body = `{"jsonrpc":"2.0","id":"` + uuid.NewString() + `","method":"tools/call","params":{"name":"` + tool + `","arguments":{}}}`
	resp := c.do(t, call)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	for _, event := range readSSEEvents(t, resp.Body, 1) {
		result, ok := event["result"].(map[string]any)
		require.True(t, ok)
		content := result["content"].([]any)
		return content[0].(map[string]any)["text"].(string)
	}
	return ""
}

func (c *credentialTestServer) requireSessionEnds(t *testing.T, sid string) {
	t.Helper()
	require.Eventually(t, func() bool { return c.bridge.session(sid) == nil }, 20*time.Second, 20*time.Millisecond)
	require.Eventually(t, func() bool {
		_, _, live := c.store.counts()
		return live == 0
	}, 20*time.Second, 20*time.Millisecond, "session credentials must be removed")
}

func TestCredentialsInitializeWithoutCredentialSpawnsNothing(t *testing.T) {
	t.Parallel()
	c := newCredentialTestServer(t, credentialServerOptions{})
	for name, call := range map[string]credentialCall{
		"no bearer":         {token: "", noCredential: true},
		"no claim":          {token: testTokenA, noCredential: true},
		"digest mismatch":   {token: testTokenA, vouched: testTokenB},
		"self owner":        {token: testTokenA, rawCredential: map[string]any{"owner": "self", "client_id": defaultTestGrant.clientID, "token_sha256": identity.TokenSHA256(testTokenA)}},
		"expired token":     {token: testTokenA, expiresAt: time.Unix(1, 0)},
		"no assertion":      {token: testTokenA, noAssertion: true},
		"forged assertion":  {token: testTokenA, badSignature: true},
		"malformed grant":   {token: testTokenA, rawCredential: map[string]any{"owner": "subject", "token_sha256": identity.TokenSHA256(testTokenA)}},
		"null credential":   {token: testTokenA, rawCredential: json.RawMessage("null")},
		"string credential": {token: testTokenA, rawCredential: "x"},
	} {
		call.body = initializeBody
		resp := c.do(t, call)
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode, name)
	}
	created, writes, _ := c.store.counts()
	require.Zero(t, created)
	require.Zero(t, writes)
}

func TestCredentialsDeliverEachUsersTokenToTheirOwnServer(t *testing.T) {
	t.Parallel()
	c := newCredentialTestServer(t, credentialServerOptions{})
	alice := credentialCall{token: testTokenA}
	bob := credentialCall{
		token:     testTokenB,
		principal: testPrincipal{subject: "user:bob", mcpServerID: defaultTestPrincipal.mcpServerID, consent: false},
		grant:     testGrant{clientID: defaultTestGrant.clientID, grantID: "00000000-0000-4000-8000-0000000000d2", generation: 1},
	}
	alice.sid = c.initialize(t, alice)
	bob.sid = c.initialize(t, bob)
	require.NotEqual(t, alice.sid, bob.sid)

	require.Equal(t, identity.TokenSHA256(testTokenA), c.toolText(t, alice, "token-sha"))
	require.Equal(t, identity.TokenSHA256(testTokenB), c.toolText(t, bob, "token-sha"))
}

func TestCredentialsRefreshRotatesTokenInSession(t *testing.T) {
	t.Parallel()
	c := newCredentialTestServer(t, credentialServerOptions{})
	call := credentialCall{token: testTokenA, expiresAt: time.Now().Add(time.Hour)}
	call.sid = c.initialize(t, call)
	require.Equal(t, identity.TokenSHA256(testTokenA), c.toolText(t, call, "token-sha"))

	call.token = testTokenB
	require.Equal(t, identity.TokenSHA256(testTokenB), c.toolText(t, call, "token-sha"), "a refreshed token replaces the file in the same session")

	// Last writer wins within one grant: a delayed request with the older
	// token, or one with the same expiry, is admitted and rewrites the file.
	call.token = testTokenA
	require.Equal(t, identity.TokenSHA256(testTokenA), c.toolText(t, call, "token-sha"))
	require.NotNil(t, c.bridge.session(call.sid))
}

func TestCredentialsOtherPrincipalCannotSeeOrEndSession(t *testing.T) {
	t.Parallel()
	c := newCredentialTestServer(t, credentialServerOptions{})
	owner := credentialCall{token: testTokenA}
	owner.sid = c.initialize(t, owner)

	others := map[string]testPrincipal{
		"other user":       {subject: "user:mallory", mcpServerID: defaultTestPrincipal.mcpServerID, consent: false},
		"other api key":    {subject: "api_key:k1", mcpServerID: defaultTestPrincipal.mcpServerID, consent: false},
		"other mcp server": {subject: defaultTestPrincipal.subject, mcpServerID: "00000000-0000-4000-8000-0000000000bb", consent: false},
		"consent session":  {subject: defaultTestPrincipal.subject, mcpServerID: defaultTestPrincipal.mcpServerID, consent: true},
	}
	for name, p := range others {
		for _, call := range []credentialCall{
			{sid: owner.sid, principal: p, token: testTokenB, body: `{"jsonrpc":"2.0","id":9,"method":"tools/list"}`},
			{sid: owner.sid, principal: p, token: testTokenB, rawCredential: "malformed", body: `{"jsonrpc":"2.0","id":9,"method":"tools/list"}`},
			{sid: owner.sid, principal: p, method: http.MethodGet},
			{sid: owner.sid, principal: p, method: http.MethodDelete},
		} {
			resp := c.do(t, call)
			require.Equal(t, http.StatusNotFound, resp.StatusCode, name+" "+call.method)
		}
	}

	require.Equal(t, identity.TokenSHA256(testTokenA), c.toolText(t, owner, "token-sha"), "the session is untouched")
	_, writes, _ := c.store.counts()
	require.Equal(t, 3, writes, "only initialize, notifications/initialized and the owner's tool call publish")
}

func TestCredentialsUnverifiedCallersTouchNothing(t *testing.T) {
	t.Parallel()
	c := newCredentialTestServer(t, credentialServerOptions{})
	owner := credentialCall{token: testTokenA}
	owner.sid = c.initialize(t, owner)

	for _, call := range []credentialCall{
		{sid: owner.sid, token: testTokenA, badSignature: true, body: `{"jsonrpc":"2.0","id":9,"method":"tools/list"}`},
		{sid: owner.sid, token: testTokenA, noAssertion: true, body: `{"jsonrpc":"2.0","id":9,"method":"tools/list"}`},
		{sid: owner.sid, badSignature: true, method: http.MethodDelete},
		{sid: owner.sid, noAssertion: true, method: http.MethodGet},
	} {
		require.Equal(t, http.StatusUnauthorized, c.do(t, call).StatusCode)
	}
	require.Equal(t, identity.TokenSHA256(testTokenA), c.toolText(t, owner, "token-sha"))
}

func TestCredentialsLostCredentialEndsSession(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*credentialCall){
		"no bearer":       func(c *credentialCall) { c.token = ""; c.noCredential = true },
		"no claim":        func(c *credentialCall) { c.noCredential = true },
		"digest mismatch": func(c *credentialCall) { c.vouched = testTokenB },
		"malformed claim": func(c *credentialCall) { c.rawCredential = map[string]any{"owner": "subject"} },
		"self owned client": func(c *credentialCall) {
			c.rawCredential = map[string]any{"owner": "self", "client_id": defaultTestGrant.clientID, "token_sha256": identity.TokenSHA256(testTokenA)}
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := newCredentialTestServer(t, credentialServerOptions{})
			call := credentialCall{token: testTokenA}
			call.sid = c.initialize(t, call)
			_, writesBefore, _ := c.store.counts()

			mutate(&call)
			call.body = `{"jsonrpc":"2.0","id":9,"method":"tools/list"}`
			require.Equal(t, http.StatusUnauthorized, c.do(t, call).StatusCode)
			c.requireSessionEnds(t, call.sid)
			_, writesAfter, _ := c.store.counts()
			require.Equal(t, writesBefore, writesAfter)
		})
	}
}

func TestCredentialsGrantChangeEndsSession(t *testing.T) {
	t.Parallel()
	for name, grant := range map[string]testGrant{
		"reauthorized":   {clientID: defaultTestGrant.clientID, grantID: defaultTestGrant.grantID, generation: 2},
		"other grant":    {clientID: defaultTestGrant.clientID, grantID: "00000000-0000-4000-8000-0000000000d9", generation: 1},
		"other client":   {clientID: "00000000-0000-4000-8000-0000000000c9", grantID: defaultTestGrant.grantID, generation: 1},
		"older reauthor": {clientID: defaultTestGrant.clientID, grantID: defaultTestGrant.grantID, generation: 3},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := newCredentialTestServer(t, credentialServerOptions{})
			call := credentialCall{token: testTokenA}
			call.sid = c.initialize(t, call)

			call.grant, call.token = grant, testTokenB
			call.body = `{"jsonrpc":"2.0","id":9,"method":"tools/list"}`
			require.Equal(t, http.StatusNotFound, c.do(t, call).StatusCode)
			c.requireSessionEnds(t, call.sid)

			fresh := credentialCall{token: testTokenB, grant: grant}
			fresh.sid = c.initialize(t, fresh)
			require.Equal(t, identity.TokenSHA256(testTokenB), c.toolText(t, fresh, "token-sha"), "the client starts over with a new server")
		})
	}
}

func TestCredentialsOwnerTeardownNeedsNoCredential(t *testing.T) {
	t.Parallel()
	c := newCredentialTestServer(t, credentialServerOptions{})
	call := credentialCall{token: testTokenA}
	call.sid = c.initialize(t, call)

	get := c.do(t, credentialCall{sid: call.sid, method: http.MethodGet, rawCredential: "malformed"})
	require.Equal(t, http.StatusOK, get.StatusCode)
	require.Equal(t, "text/event-stream", get.Header.Get("Content-Type"))
	_ = get.Body.Close()

	require.Equal(t, http.StatusNoContent, c.do(t, credentialCall{sid: call.sid, method: http.MethodDelete, rawCredential: "malformed"}).StatusCode)
	c.requireSessionEnds(t, call.sid)
}

func TestCredentialsConsentSessionEnforcesAllowedMethods(t *testing.T) {
	t.Parallel()
	c := newCredentialTestServer(t, credentialServerOptions{})
	consent := testPrincipal{subject: defaultTestPrincipal.subject, mcpServerID: defaultTestPrincipal.mcpServerID, consent: true}
	call := credentialCall{token: testTokenA, principal: consent}
	call.sid = c.initialize(t, call)

	call.body = `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
	require.Equal(t, http.StatusOK, c.do(t, call).StatusCode)

	for _, body := range []string{
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo","arguments":{}}}`,
		`[{"jsonrpc":"2.0","id":4,"method":"tools/list"},{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"echo"}}]`,
		`{"jsonrpc":"2.0","id":"srv-1","result":{}}`,
	} {
		call.body = body
		require.Equal(t, http.StatusForbidden, c.do(t, call).StatusCode, body)
	}
	_, writes, _ := c.store.counts()
	require.Equal(t, 3, writes, "refused methods publish nothing")
	require.NotNil(t, c.bridge.session(call.sid))

	call.token, call.noCredential = "", true
	call.body = `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"echo"}}`
	require.Equal(t, http.StatusUnauthorized, c.do(t, call).StatusCode, "the credential is judged before the method")
	c.requireSessionEnds(t, call.sid)
}

func TestCredentialsChildEnvironmentIsIsolated(t *testing.T) {
	t.Parallel()
	exe, err := os.Executable()
	require.NoError(t, err)
	base := childEnv(append(os.Environ(), "TUNNEL_KEY=secret-tunnel-key", "OKTA_ACCESS_TOKEN_FILE=/shared/token", "HOME=/shared/home", "INHERITED_SETTING=kept"))
	c := newCredentialTestServer(t, credentialServerOptions{command: stdioFixtureEnv + "=1 '" + exe + "'", env: base})
	call := credentialCall{token: testTokenA}
	call.sid = c.initialize(t, call)

	var env map[string]string
	require.NoError(t, json.Unmarshal([]byte(c.toolText(t, call, "env")), &env))
	sess := c.bridge.session(call.sid)
	require.Equal(t, sess.cred.dir.tokenPath(), env[AccessTokenFileEnv])
	require.Equal(t, sess.cred.dir.homePath(), env["HOME"])
	require.Equal(t, sess.cred.dir.homePath()+"/.config", env["XDG_CONFIG_HOME"])
	require.NotContains(t, env, "OKTA_ACCESS_TOKEN_FILE")
	require.NotContains(t, env, "TUNNEL_KEY")
	require.Equal(t, "kept", env["INHERITED_SETTING"])
}

func TestCredentialsServerStderrIsNotLogged(t *testing.T) {
	t.Parallel()
	c := newCredentialTestServer(t, credentialServerOptions{})
	call := credentialCall{token: testTokenA}
	call.sid = c.initialize(t, call)
	require.Equal(t, "leaked", c.toolText(t, call, "leak"))
	require.Equal(t, http.StatusNoContent, c.do(t, credentialCall{sid: call.sid, method: http.MethodDelete}).StatusCode)
	c.requireSessionEnds(t, call.sid)
	require.Eventually(t, func() bool { return strings.Contains(c.logs.String(), "not logged in credentials mode") }, 10*time.Second, 20*time.Millisecond)
	require.NotContains(t, c.logs.String(), testTokenA)
	require.NotContains(t, c.logs.String(), "fixture server starting")
}

func TestCredentialsExpiryEndsSessionWithoutActivity(t *testing.T) {
	t.Parallel()
	c := newCredentialTestServer(t, credentialServerOptions{expiryGrace: 50 * time.Millisecond})
	call := credentialCall{token: testTokenA, expiresAt: time.Now().Add(1500 * time.Millisecond)}
	call.sid = c.initialize(t, call)
	c.requireSessionEnds(t, call.sid)
}

func TestCredentialsMaxAgeEndsSessionWithUnknownExpiry(t *testing.T) {
	t.Parallel()
	c := newCredentialTestServer(t, credentialServerOptions{maxAge: 500 * time.Millisecond})
	call := credentialCall{token: testTokenA}
	call.sid = c.initialize(t, call)
	// The GET stream keeps no session alive past its credential.
	get := c.do(t, credentialCall{sid: call.sid, method: http.MethodGet})
	require.Equal(t, http.StatusOK, get.StatusCode)
	c.requireSessionEnds(t, call.sid)
}

func TestCredentialsStaleExpiryDoesNotEndRefreshedSession(t *testing.T) {
	t.Parallel()
	c := newCredentialTestServer(t, credentialServerOptions{})
	call := credentialCall{token: testTokenA}
	call.sid = c.initialize(t, call)
	sess := c.bridge.session(call.sid)
	require.True(t, sess.enterGate(t.Context()))
	stale := sess.cred.generation
	sess.leaveGate()

	call.token = testTokenB
	require.Equal(t, identity.TokenSHA256(testTokenB), c.toolText(t, call, "token-sha"))
	sess.expireCredential(stale)
	require.False(t, sess.closing.Load(), "a superseded expiry is ignored")
}

func TestCredentialsTerminationOrdersAfterAdmittedWrite(t *testing.T) {
	t.Parallel()
	c := newCredentialTestServer(t, credentialServerOptions{})
	call := credentialCall{token: testTokenA}
	call.sid = c.initialize(t, call)
	sess := c.bridge.session(call.sid)

	published := make(chan struct{})
	resume := make(chan struct{})
	require.True(t, sess.enterGate(t.Context()))
	sess.cred.afterPublish = func() {
		close(published)
		<-resume
	}
	sess.leaveGate()

	posted := make(chan int, 1)
	go func() {
		call.token = testTokenB
		call.body = `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"echo","arguments":{"text":"x"}}}`
		posted <- c.do(t, call).StatusCode
	}()
	<-published

	deleted := make(chan int, 1)
	go func() { deleted <- c.do(t, credentialCall{sid: call.sid, method: http.MethodDelete}).StatusCode }()
	select {
	case <-deleted:
		t.Fatal("termination must wait for the admitted write")
	case <-time.After(200 * time.Millisecond):
	}
	require.False(t, sess.closing.Load())
	close(resume)
	require.Equal(t, http.StatusOK, <-posted)
	require.Equal(t, http.StatusNoContent, <-deleted)
	_, writes, _ := c.store.counts()

	call.body = `{"jsonrpc":"2.0","id":8,"method":"tools/list"}`
	require.Equal(t, http.StatusNotFound, c.do(t, call).StatusCode)
	_, writesAfter, _ := c.store.counts()
	require.Equal(t, writes, writesAfter, "nothing is published after termination")
	c.requireSessionEnds(t, call.sid)
}

func TestCredentialsExpiryWhileQueuedIsRefusedWithoutTeardown(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		expiresIn time.Duration
		advance   time.Duration
	}{
		"token expires":     {expiresIn: 10 * time.Second, advance: 15 * time.Second},
		"assertion expires": {expiresIn: 0, advance: 2 * time.Minute},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := newCredentialTestServer(t, credentialServerOptions{})
			call := credentialCall{token: testTokenA}
			call.sid = c.initialize(t, call)
			sess := c.bridge.session(call.sid)
			_, writes, _ := c.store.counts()

			before := sessionCredentialState(t, sess)
			require.True(t, sess.enterGate(t.Context()))
			status := make(chan int, 1)
			go func() {
				queued := call
				queued.token = testTokenB
				if tc.expiresIn > 0 {
					queued.expiresAt = c.clock.Now().Add(tc.expiresIn)
				}
				queued.body = `{"jsonrpc":"2.0","id":7,"method":"tools/list"}`
				status <- c.do(t, queued).StatusCode
			}()
			// Wait until the request is queued behind the gate.
			require.Eventually(t, func() bool {
				sess.mu.Lock()
				defer sess.mu.Unlock()
				return sess.inflight > 0
			}, 10*time.Second, 10*time.Millisecond)
			c.clock.Advance(tc.advance)
			sess.leaveGate()

			require.Equal(t, http.StatusUnauthorized, <-status)
			_, writesAfter, _ := c.store.counts()
			require.Equal(t, writes, writesAfter, "nothing is published")
			require.Equal(t, before, sessionCredentialState(t, sess), "the deadline is not renewed")
			require.False(t, sess.closing.Load(), "a stale request does not end the session")
			require.Equal(t, identity.TokenSHA256(testTokenA), c.toolText(t, call, "token-sha"))
		})
	}
}

func TestCredentialsDelayedExpiredTokenKeepsNewerToken(t *testing.T) {
	t.Parallel()
	c := newCredentialTestServer(t, credentialServerOptions{})
	call := credentialCall{token: testTokenA}
	call.sid = c.initialize(t, call)

	call.token = testTokenB
	require.Equal(t, identity.TokenSHA256(testTokenB), c.toolText(t, call, "token-sha"))

	sess := c.bridge.session(call.sid)
	before := sessionCredentialState(t, sess)
	_, writes, _ := c.store.counts()

	delayed := call
	delayed.token, delayed.expiresAt = testTokenA, time.Unix(1, 0)
	delayed.body = `{"jsonrpc":"2.0","id":8,"method":"tools/list"}`
	require.Equal(t, http.StatusUnauthorized, c.do(t, delayed).StatusCode)

	content, err := os.ReadFile(sess.cred.dir.tokenPath())
	require.NoError(t, err)
	require.Equal(t, identity.TokenSHA256(testTokenB), identity.TokenSHA256(string(content)), "the newer token stays")
	_, writesAfter, _ := c.store.counts()
	require.Equal(t, writes, writesAfter)
	require.Equal(t, before, sessionCredentialState(t, sess), "the deadline is untouched")
}

// credentialState is what a publication changes: its generation and timer.
type credentialState struct {
	generation uint64
	timer      *time.Timer
}

func sessionCredentialState(t *testing.T, sess *stdioSession) credentialState {
	t.Helper()
	require.True(t, sess.enterGate(t.Context()))
	defer sess.leaveGate()
	return credentialState{generation: sess.cred.generation, timer: sess.cred.timer}
}

func TestCredentialsExpiredRequestsDoNotPostponeTheDeadline(t *testing.T) {
	t.Parallel()
	c := newCredentialTestServer(t, credentialServerOptions{maxAge: time.Second})
	call := credentialCall{token: testTokenA}
	call.sid = c.initialize(t, call)

	sess := c.bridge.session(call.sid)
	initial := sessionCredentialState(t, sess)
	expired := call
	expired.expiresAt = time.Unix(1, 0)
	expired.body = `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	// The one-second deadline must fire while refusals keep arriving; the
	// bound leaves room for the shutdown sequence but not for renewals.
	ended := time.Now().Add(10 * time.Second)
	for !sess.closing.Load() {
		require.True(t, time.Now().Before(ended), "refusals must not postpone the deadline")
		require.Contains(t, []int{http.StatusUnauthorized, http.StatusNotFound}, c.do(t, expired).StatusCode)
		if !sess.closing.Load() {
			require.Equal(t, initial, sessionCredentialState(t, sess))
		}
		time.Sleep(50 * time.Millisecond)
	}
	c.requireSessionEnds(t, call.sid)
}

func TestCredentialsExpiredTokenFromAnotherGrantEndsSession(t *testing.T) {
	t.Parallel()
	c := newCredentialTestServer(t, credentialServerOptions{})
	call := credentialCall{token: testTokenA}
	call.sid = c.initialize(t, call)

	call.grant = testGrant{clientID: defaultTestGrant.clientID, grantID: defaultTestGrant.grantID, generation: 2}
	call.token, call.expiresAt = testTokenB, time.Unix(1, 0)
	call.body = `{"jsonrpc":"2.0","id":8,"method":"tools/list"}`
	require.Equal(t, http.StatusNotFound, c.do(t, call).StatusCode)
	c.requireSessionEnds(t, call.sid)
}

func TestCredentialsServerExitStopsPublishing(t *testing.T) {
	t.Parallel()
	c := newCredentialTestServer(t, credentialServerOptions{})
	call := credentialCall{token: testTokenA}
	call.sid = c.initialize(t, call)
	sess := c.bridge.session(call.sid)
	call.body = `{"jsonrpc":"2.0","id":"c","method":"tools/call","params":{"name":"crash"}}`
	_ = c.do(t, call)
	require.Eventually(t, sess.closing.Load, 10*time.Second, 10*time.Millisecond)
	_, writes, _ := c.store.counts()

	call.body = `{"jsonrpc":"2.0","id":8,"method":"tools/list"}`
	require.Equal(t, http.StatusNotFound, c.do(t, call).StatusCode)
	_, writesAfter, _ := c.store.counts()
	require.Equal(t, writes, writesAfter)
	c.requireSessionEnds(t, call.sid)
}

func TestCredentialsFailedInitializeRemovesCredentials(t *testing.T) {
	t.Parallel()
	c := newCredentialTestServer(t, credentialServerOptions{command: "exit 0"})
	for attempt := 1; attempt <= 3; attempt++ {
		resp := c.do(t, credentialCall{token: testTokenA, body: initializeBody})
		require.Contains(t, []int{http.StatusBadGateway, http.StatusGatewayTimeout}, resp.StatusCode)
		require.Eventually(t, func() bool {
			created, _, live := c.store.counts()
			return created == attempt && live == 0
		}, 20*time.Second, 20*time.Millisecond, "a server that exits at once leaves no credentials and blocks nothing")
	}
	closed := make(chan struct{})
	go func() {
		c.bridge.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(20 * time.Second):
		t.Fatal("bridge shutdown must not hang after failed starts")
	}
}

func TestCredentialsForwardFailureEndsSession(t *testing.T) {
	t.Parallel()
	c := newCredentialTestServer(t, credentialServerOptions{})
	call := credentialCall{token: testTokenA}
	call.sid = c.initialize(t, call)
	require.Equal(t, "closed", c.toolText(t, call, "close-stdin"))
	sess := c.bridge.session(call.sid)
	_, writes, _ := c.store.counts()

	call.body = `{"jsonrpc":"2.0","id":8,"method":"tools/list"}`
	require.Equal(t, http.StatusNotFound, c.do(t, call).StatusCode)
	require.True(t, sess.closing.Load(), "a request that cannot be forwarded ends the session")
	require.Equal(t, http.StatusNotFound, c.do(t, call).StatusCode)
	_, writesAfter, _ := c.store.counts()
	require.Equal(t, writes+1, writesAfter, "only the failed request published")
	c.requireSessionEnds(t, call.sid)
}

func TestCredentialsCleanupWaitsForAdmittedPublisher(t *testing.T) {
	t.Parallel()
	c := newCredentialTestServer(t, credentialServerOptions{})
	call := credentialCall{token: testTokenA}
	call.sid = c.initialize(t, call)
	sess := c.bridge.session(call.sid)

	published := make(chan struct{})
	resume := make(chan struct{})
	require.True(t, sess.enterGate(t.Context()))
	sess.cred.afterPublish = func() {
		close(published)
		<-resume
	}
	sess.leaveGate()

	posted := make(chan int, 1)
	go func() {
		call.token = testTokenB
		call.body = `{"jsonrpc":"2.0","id":7,"method":"tools/list"}`
		posted <- c.do(t, call).StatusCode
	}()
	<-published
	// The server dies while the publisher holds the gate.
	require.NoError(t, sess.cmd.Process.Kill())
	<-sess.terminated
	require.Never(t, func() bool {
		_, _, live := c.store.counts()
		return live == 0
	}, 300*time.Millisecond, 20*time.Millisecond, "storage outlives the publisher using it")

	close(resume)
	<-posted
	c.requireSessionEnds(t, call.sid)
}

func TestCredentialsRemovedEvenWhenGroupSurvives(t *testing.T) {
	t.Parallel()
	c := newCredentialTestServer(t, credentialServerOptions{})
	call := credentialCall{token: testTokenA}
	call.sid = c.initialize(t, call)
	sess := c.bridge.session(call.sid)
	// Stand in for a process the kernel will not stop.
	sess.groupAlive = func(time.Duration) bool { return true }

	require.Equal(t, http.StatusNoContent, c.do(t, credentialCall{sid: call.sid, method: http.MethodDelete}).StatusCode)
	c.requireSessionEnds(t, call.sid)
	require.Contains(t, c.logs.String(), "survived shutdown")
}

func TestCredentialsConcurrentRequestsAndRefresh(t *testing.T) {
	t.Parallel()
	c := newCredentialTestServer(t, credentialServerOptions{})
	call := credentialCall{token: testTokenA}
	call.sid = c.initialize(t, call)

	get := c.do(t, credentialCall{sid: call.sid, method: http.MethodGet})
	require.Equal(t, http.StatusOK, get.StatusCode)

	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			req := call
			if i%2 == 0 {
				req.token = testTokenB
			}
			text := c.toolText(t, req, "token-sha")
			require.Contains(t, []string{identity.TokenSHA256(testTokenA), identity.TokenSHA256(testTokenB)}, text)
		})
	}
	wg.Wait()
	require.NotNil(t, c.bridge.session(call.sid))
}

func TestCredentialsTokenWriteFailureEndsSession(t *testing.T) {
	t.Parallel()
	c := newCredentialTestServer(t, credentialServerOptions{})
	call := credentialCall{token: testTokenA}
	call.sid = c.initialize(t, call)

	c.store.failWrites.Store(true)
	call.body = `{"jsonrpc":"2.0","id":8,"method":"tools/list"}`
	require.Equal(t, http.StatusNotFound, c.do(t, call).StatusCode, "nothing is forwarded without its token")
	c.requireSessionEnds(t, call.sid)

	resp := c.do(t, credentialCall{token: testTokenA, body: initializeBody})
	require.Equal(t, http.StatusBadGateway, resp.StatusCode, "a server never starts without its token file")
	created, _, live := c.store.counts()
	require.Equal(t, 2, created)
	require.Zero(t, live)
}

func TestCredentialsSessionStopsAdmittingOnceTerminationBegins(t *testing.T) {
	t.Parallel()
	c := newCredentialTestServer(t, credentialServerOptions{})
	call := credentialCall{token: testTokenA}
	call.sid = c.initialize(t, call)
	sess := c.bridge.session(call.sid)

	// Hold the gate so termination cannot finish yet.
	require.True(t, sess.enterGate(t.Context()))
	changed := call
	changed.grant = testGrant{clientID: defaultTestGrant.clientID, grantID: defaultTestGrant.grantID, generation: 2}
	changed.body = `{"jsonrpc":"2.0","id":8,"method":"tools/list"}`
	require.Equal(t, http.StatusNotFound, c.do(t, changed).StatusCode)
	require.True(t, sess.closing.Load(), "admission stops before termination runs")
	_, writes, _ := c.store.counts()
	sess.leaveGate()

	call.body = `{"jsonrpc":"2.0","id":9,"method":"tools/list"}`
	require.Equal(t, http.StatusNotFound, c.do(t, call).StatusCode, "the old grant is not admitted meanwhile")
	_, writesAfter, _ := c.store.counts()
	require.Equal(t, writes, writesAfter)
	c.requireSessionEnds(t, call.sid)
}
