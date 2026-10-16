package channels

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var channelsInfra *testenv.Environment

func TestMain(m *testing.M) {
	infra, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatalf("launch channels test infrastructure: %v", err)
	}
	channelsInfra = infra
	code := m.Run()
	if err := cleanup(); err != nil {
		log.Fatalf("cleanup channels test infrastructure: %v", err)
	}
	os.Exit(code)
}

// fixture is one isolated database with two organizations and a handler whose
// authenticator resolves the Authorization header to a named identity, so a
// test can change someone's permissions mid-stream the way revocation does.
type fixture struct {
	db        *pgxpool.Pool
	handler   *Handler
	server    *httptest.Server
	mu        *sync.Mutex
	identitie map[string]Principal
}

const (
	testOrg  = "channels-test-org"
	otherOrg = "channels-other-org"
)

func newFixture(t *testing.T, name string) *fixture {
	t.Helper()
	db := newDatabase(t, name)
	f := newFixtureOn(t, db)
	return f
}

func newDatabase(t *testing.T, name string) *pgxpool.Pool {
	t.Helper()
	ctx := t.Context()
	db, err := channelsInfra.CloneTestDatabase(t, name)
	require.NoError(t, err)
	organizations := orgrepo.New(db)
	projects := projectsrepo.New(db)
	for _, id := range []string{testOrg, otherOrg} {
		_, err = organizations.UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
			ID: id, Name: id, Slug: id,
			WorkosID: pgtype.Text{String: "", Valid: false}, Whitelisted: pgtype.Bool{Bool: false, Valid: false},
			CreationSource: pgtype.Text{String: "", Valid: false},
		})
		require.NoError(t, err)
		// Channels stores an organization's rows under its canonical default
		// project; the extra project proves a second one never claims them.
		for _, slug := range []string{"other", "default"} {
			_, err = projects.CreateProject(ctx, projectsrepo.CreateProjectParams{
				Name: slug, Slug: slug, OrganizationID: id,
			})
			require.NoError(t, err)
		}
	}
	return db
}

// newFixtureOn builds another handler over an existing database, which is how
// delivery across separate handler instances is tested.
func newFixtureOn(t *testing.T, db *pgxpool.Pool) *fixture {
	t.Helper()
	f := &fixture{
		db: db, handler: nil, server: nil,
		mu: &sync.Mutex{},
		identitie: map[string]Principal{
			"alice":    {OrganizationID: testOrg, SubjectID: "alice", SubjectName: "Alice Example", CanConnect: true, CanAdmin: true, CanReadAll: false},
			"bob":      {OrganizationID: testOrg, SubjectID: "bob", SubjectName: "Bob Example", CanConnect: true, CanAdmin: false, CanReadAll: false},
			"outsider": {OrganizationID: testOrg, SubjectID: "outsider", SubjectName: "Outsider", CanConnect: true, CanAdmin: false, CanReadAll: false},
			"observer": {OrganizationID: testOrg, SubjectID: "observer", SubjectName: "Observer", CanConnect: true, CanAdmin: true, CanReadAll: true},
			"stranger": {OrganizationID: otherOrg, SubjectID: "alice", SubjectName: "Alice Elsewhere", CanConnect: true, CanAdmin: true, CanReadAll: true},
		},
	}
	// Tests exercise the write path far faster than a person does; the burst
	// limit has its own test which installs its own limiter.
	limits := DefaultLimits()
	limits.WritesPerMinute, limits.WriteBurst = 100000, 100000
	f.handler = New(db, f.authenticate, WithLogger(testenv.NewLogger(t)), WithLimits(limits))
	f.server = httptest.NewServer(f.handler)
	t.Cleanup(func() {
		f.handler.Close()
		f.server.Close()
	})
	return f
}

func (f *fixture) authenticate(r *http.Request) (Principal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.identitie[r.Header.Get("Authorization")]
	if !ok {
		return Principal{}, fmt.Errorf("unknown identity")
	}
	return p, nil
}

// request sends one browser API operation without asserting anything, so a
// goroutine can call it; only the test goroutine may assert.
func (f *fixture) request(ctx context.Context, who, operation string, args map[string]any) (map[string]any, int, error) {
	raw, err := json.Marshal(map[string]any{"operation": operation, "arguments": args})
	if err != nil {
		return nil, 0, fmt.Errorf("encode operation: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.server.URL+"/channels/api", bytes.NewReader(raw))
	if err != nil {
		return nil, 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", who)
	res, err := f.server.Client().Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("send request: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	var decoded map[string]any
	if err := json.NewDecoder(res.Body).Decode(&decoded); err != nil {
		return nil, res.StatusCode, fmt.Errorf("decode response: %w", err)
	}
	return decoded, res.StatusCode, nil
}

// post sends one browser API operation and requires it to succeed.
func (f *fixture) post(t *testing.T, who, operation string, args map[string]any) map[string]any {
	t.Helper()
	body, status := f.try(t, who, operation, args)
	require.Equal(t, http.StatusOK, status, "%s %s: %v", who, operation, body)
	return body
}

// try sends one browser API operation and returns its body and status.
func (f *fixture) try(t *testing.T, who, operation string, args map[string]any) (map[string]any, int) {
	t.Helper()
	body, status, err := f.request(t.Context(), who, operation, args)
	require.NoError(t, err)
	return body, status
}

func (f *fixture) snapshot(t *testing.T, who string) map[string]any {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, f.server.URL+"/channels/api", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", who)
	res, err := f.server.Client().Do(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, res.Body.Close()) }()
	var decoded map[string]any
	require.NoError(t, json.NewDecoder(res.Body).Decode(&decoded))
	require.Equal(t, http.StatusOK, res.StatusCode, "snapshot for %s: %v", who, decoded)
	return decoded
}

func (f *fixture) rpc(t *testing.T, who, method string, params any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "rpc", "method": method, "params": params})
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, f.server.URL+"/channels/mcp", bytes.NewReader(raw))
	require.NoError(t, err)
	req.Header.Set("Authorization", who)
	res, err := f.server.Client().Do(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, res.Body.Close()) }()
	var decoded map[string]any
	require.NoError(t, json.NewDecoder(res.Body).Decode(&decoded))
	return decoded
}

// enable turns Channels on for an organization, which is off by default.
func (f *fixture) enable(t *testing.T, who string) {
	t.Helper()
	f.post(t, who, "channels_admin", map[string]any{"enabled": true})
}

// connect opens an agent instance and returns it.
func (f *fixture) connect(t *testing.T, who, label string) map[string]any {
	t.Helper()
	return f.post(t, who, "channels_connect", map[string]any{"label": label})
}

// eventStream decodes one SSE subscription's events.
type eventStream struct {
	events chan map[string]any
	close  func()
}

// listen opens one SSE subscription and decodes its events.
func (f *fixture) listen(t *testing.T, who string, id any, uris ...string) *eventStream {
	t.Helper()
	filter := map[string]any{}
	if len(uris) > 0 {
		filter["resourceSubscriptions"] = uris
	}
	raw, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": "subscriptions/listen", "params": map[string]any{"notifications": filter}})
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(context.WithoutCancel(t.Context()), http.MethodPost, f.server.URL+"/channels/mcp", bytes.NewReader(raw))
	require.NoError(t, err)
	req.Header.Set("Authorization", who)
	res, err := f.server.Client().Do(req) //nolint:bodyclose // the returned stream closes it from t.Cleanup.
	require.NoError(t, err)
	events := make(chan map[string]any, 64)
	go func() {
		defer close(events)
		scanner := bufio.NewScanner(res.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var decoded map[string]any
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &decoded) == nil {
				events <- decoded
			}
		}
	}()
	s := &eventStream{events: events, close: func() { _ = res.Body.Close() }}
	t.Cleanup(s.close)
	return s
}

func (s *eventStream) next(t *testing.T) map[string]any {
	t.Helper()
	select {
	case v, ok := <-s.events:
		require.True(t, ok, "stream ended unexpectedly")
		return v
	case <-time.After(20 * time.Second):
		t.Fatal("timed out waiting for a stream event")
		return nil
	}
}

func (s *eventStream) silent(t *testing.T, window time.Duration) {
	t.Helper()
	select {
	case v := <-s.events:
		t.Fatalf("expected no event, received %v", v)
	case <-time.After(window):
	}
}

// The accessors below keep decoded JSON assertions checked and keep numeric
// comparisons integral, because every identifier and cursor here is a whole
// number that merely travelled as JSON.
func mapOf(t *testing.T, value any) map[string]any {
	t.Helper()
	decoded, ok := value.(map[string]any)
	require.True(t, ok, "expected an object, got %T", value)
	return decoded
}

func intOf(t *testing.T, value any) int64 {
	t.Helper()
	number, ok := value.(float64)
	require.True(t, ok, "expected a number, got %T", value)
	return int64(number)
}

func boolOf(t *testing.T, value any) bool {
	t.Helper()
	decoded, ok := value.(bool)
	require.True(t, ok, "expected a boolean, got %T", value)
	return decoded
}

func stringOf(t *testing.T, value any) string {
	t.Helper()
	decoded, ok := value.(string)
	require.True(t, ok, "expected a string, got %T", value)
	return decoded
}

// cancel sends notifications/cancelled, optionally naming the stream with the
// server-issued token from its acknowledgment.
func (f *fixture) cancel(t *testing.T, who string, requestID any, token string) {
	t.Helper()
	params := map[string]any{"requestId": requestID}
	if token != "" {
		params["_meta"] = map[string]any{subscriptionTokenMeta: token}
	}
	raw, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "notifications/cancelled", "params": params})
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, f.server.URL+"/channels/mcp", bytes.NewReader(raw))
	require.NoError(t, err)
	req.Header.Set("Authorization", who)
	res, err := f.server.Client().Do(req)
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())
	require.Equal(t, http.StatusAccepted, res.StatusCode)
}

// subscriptionToken reads the server-issued stream handle out of an
// acknowledgment.
func subscriptionToken(t *testing.T, ack map[string]any) string {
	t.Helper()
	meta := mapOf(t, mapOf(t, ack["params"])["_meta"])
	token, _ := meta[subscriptionTokenMeta].(string)
	return token
}

func messageIDs(t *testing.T, payload any) []int64 {
	t.Helper()
	items, ok := payload.([]any)
	require.True(t, ok, "expected a message list, got %T", payload)
	out := make([]int64, 0, len(items))
	for _, item := range items {
		out = append(out, intOf(t, mapOf(t, item)["id"]))
	}
	return out
}

func mustUUID(t *testing.T, value any) uuid.UUID {
	t.Helper()
	parsed, err := uuid.Parse(stringOf(t, value))
	require.NoError(t, err)
	return parsed
}
