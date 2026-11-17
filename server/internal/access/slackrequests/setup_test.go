package slackrequests_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/access/slackrequests"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orghost"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections"
	slackrepo "github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	slackapi "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/api"
	userrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

const (
	testTeamID   = "TEXAMPLE01"
	testBotToken = "xoxb-synthetic"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
	}
	infra = res
	code := m.Run()
	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
	}
	os.Exit(code)
}

// fixture is an organization with an administrator, a requester and a fake
// Slack workspace both are in.
type fixture struct {
	db             *pgxpool.Pool
	cards          *slackrequests.Cards
	notifier       *slackrequests.Notifier
	slack          *fakeSlack
	workspaces     *fakeWorkspaces
	organizationID string
	adminID        string
	requesterID    string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := t.Context()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("gram-local"), billing.NewStubClient(logger, tracerProvider))
	authCtx, ok := contextvalues.GetAuthContext(testenv.InitAuthContext(t, ctx, conn, sessionManager))
	require.True(t, ok)

	serverURL, err := url.Parse("https://api.speakeasy.test")
	require.NoError(t, err)
	siteURL, err := url.Parse("https://app.speakeasy.test")
	require.NoError(t, err)
	cards := slackrequests.NewCards(conn, orghost.New(orghost.Config{ServerURL: serverURL, SiteURL: siteURL, PlatformHosts: nil, LegacyDefaultHost: nil, NewOrganizationDefaultHost: nil}))
	slack := newFakeSlack(t)
	workspaces := &fakeWorkspaces{workspaces: []slackdirectoryconnections.MessagingWorkspace{{ConnectionID: uuid.New(), TeamID: testTeamID, Token: testBotToken}}}
	f := &fixture{
		db:             conn,
		cards:          cards,
		notifier:       slackrequests.NewNotifier(logger, cards, workspaces, slackapi.NewClient(slack.server.URL, slack.server.Client())),
		slack:          slack,
		workspaces:     workspaces,
		organizationID: authCtx.ActiveOrganizationID,
		adminID:        "",
		requesterID:    "",
	}
	f.connectWorkspace(t)
	var adminEmail, requesterEmail string
	f.adminID, adminEmail = f.addMember(t, "Grace Admin")
	f.requesterID, requesterEmail = f.addMember(t, "Ada Member")
	f.makeAdmin(t, f.adminID)
	slack.mapUser(adminEmail, "UADMIN")
	slack.mapUser(requesterEmail, "UREQUESTER")
	return f
}

func (f *fixture) addMember(t *testing.T, name string) (string, string) {
	t.Helper()
	userID := "user_" + uuid.NewString()
	email := userID + "@example.com"
	_, err := userrepo.New(f.db).UpsertUser(t.Context(), userrepo.UpsertUserParams{ID: userID, Email: email, DisplayName: name, PhotoUrl: conv.PtrToPGText(nil), Admin: false})
	require.NoError(t, err)
	_, err = orgrepo.New(f.db).UpsertOrganizationUserRelationship(t.Context(), orgrepo.UpsertOrganizationUserRelationshipParams{OrganizationID: f.organizationID, UserID: conv.ToPGText(userID)})
	require.NoError(t, err)
	return userID, email
}

// makeAdmin gives the member the organization's admin role.
func (f *fixture) makeAdmin(t *testing.T, userID string) {
	t.Helper()
	ctx := t.Context()
	now := time.Now().UTC()
	_, err := repo.New(f.db).CreateOrganizationRole(ctx, repo.CreateOrganizationRoleParams{
		OrganizationID: f.organizationID, WorkosSlug: "admin", WorkosName: "Admin", WorkosDescription: conv.ToPGTextEmpty(""),
		WorkosCreatedAt: conv.ToPGTimestamptz(now), WorkosUpdatedAt: conv.ToPGTimestamptz(now), WorkosLastEventID: conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	_, err = repo.New(f.db).UpsertOrganizationRoleAssignment(ctx, repo.UpsertOrganizationRoleAssignmentParams{
		OrganizationID: f.organizationID, WorkosUserID: "workos_" + userID, UserID: conv.ToPGText(userID),
		WorkosMembershipID: conv.ToPGTextEmpty(""), WorkosUpdatedAt: conv.ToPGTimestamptz(now), WorkosLastEventID: conv.ToPGTextEmpty(""), WorkosRoleSlug: "admin",
	})
	require.NoError(t, err)
}

func (f *fixture) connectWorkspace(t *testing.T) {
	t.Helper()
	_, err := slackrepo.New(f.db).CreateSlackDirectoryConnection(t.Context(), slackrepo.CreateSlackDirectoryConnectionParams{
		OrganizationID: f.organizationID, SlackTeamID: testTeamID, SlackTeamName: conv.ToPGText("Example workspace"),
		CredentialsEncrypted: conv.ToPGTextEmpty(""), GrantedScopes: []string{}, Generation: uuid.New(),
	})
	require.NoError(t, err)
}

// pendingRequest stores a pending chat:read request from the requester.
func (f *fixture) pendingRequest(t *testing.T) slackrequests.Target {
	t.Helper()
	row, err := repo.New(f.db).InsertAccessRequest(t.Context(), repo.InsertAccessRequestParams{
		OrganizationID:  f.organizationID,
		RequesterUserID: f.requesterID,
		Scope:           "chat:read",
		Selector:        []byte(`{"resource_kind":"chat","resource_id":"*"}`),
		ResourceName:    conv.ToPGTextEmpty(""),
		Message:         conv.ToPGText("Investigating a reported session"),
		ChallengeIds:    []string{},
		ExpiresAt:       conv.ToPGTimestamptz(time.Now().Add(24 * time.Hour)),
	})
	require.NoError(t, err)
	return slackrequests.Target{OrganizationID: f.organizationID, RequestID: row.ID}
}

// decide records a decision the way access.Service does.
func (f *fixture) decide(t *testing.T, target slackrequests.Target, status string) {
	t.Helper()
	_, err := repo.New(f.db).DecideAccessRequest(t.Context(), repo.DecideAccessRequestParams{
		Status: status, DecidedByUserID: conv.ToPGText(f.adminID), DecidedVia: conv.ToPGText("slack"),
		TemporaryGrantID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, OrganizationID: target.OrganizationID, ID: target.RequestID,
	})
	require.NoError(t, err)
}

func (f *fixture) messages(t *testing.T, target slackrequests.Target) []repo.AccessRequestMessage {
	t.Helper()
	rows, err := repo.New(f.db).ListAccessRequestMessages(t.Context(), repo.ListAccessRequestMessagesParams{OrganizationID: target.OrganizationID, AccessRequestID: target.RequestID})
	require.NoError(t, err)
	return rows
}

type fakeWorkspaces struct {
	workspaces []slackdirectoryconnections.MessagingWorkspace
}

func (w *fakeWorkspaces) MessagingWorkspaces(context.Context, string, string) ([]slackdirectoryconnections.MessagingWorkspace, error) {
	return w.workspaces, nil
}

type slackCall struct {
	Method string
	Form   url.Values
}

// fakeSlack serves the Slack Web API methods the notifier calls.
type fakeSlack struct {
	server *httptest.Server
	mu     sync.Mutex
	calls  []slackCall
	users  map[string]string
	zones  map[string]string
	nextTS int
}

func newFakeSlack(t *testing.T) *fakeSlack {
	t.Helper()
	fake := &fakeSlack{server: nil, mu: sync.Mutex{}, calls: nil, users: map[string]string{}, zones: map[string]string{}, nextTS: 0}
	mux := http.NewServeMux()
	mux.HandleFunc("/users.lookupByEmail", func(w http.ResponseWriter, r *http.Request) {
		form := fake.record(r, "users.lookupByEmail")
		fake.mu.Lock()
		id, ok := fake.users[form.Get("email")]
		fake.mu.Unlock()
		if !ok {
			writeJSON(w, map[string]any{"ok": false, "error": "users_not_found"})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "user": map[string]any{"id": id, "is_email_confirmed": true}})
	})
	mux.HandleFunc("/users.info", func(w http.ResponseWriter, r *http.Request) {
		form := fake.record(r, "users.info")
		fake.mu.Lock()
		zone := fake.zones[form.Get("user")]
		fake.mu.Unlock()
		writeJSON(w, map[string]any{"ok": true, "user": map[string]any{"id": form.Get("user"), "tz": zone}})
	})
	mux.HandleFunc("/chat.postMessage", func(w http.ResponseWriter, r *http.Request) {
		form := fake.record(r, "chat.postMessage")
		fake.mu.Lock()
		fake.nextTS++
		ts := fmt.Sprintf("1700000000.%06d", fake.nextTS)
		fake.mu.Unlock()
		writeJSON(w, map[string]any{"ok": true, "channel": "D" + form.Get("channel"), "ts": ts})
	})
	mux.HandleFunc("/chat.update", func(w http.ResponseWriter, r *http.Request) {
		form := fake.record(r, "chat.update")
		writeJSON(w, map[string]any{"ok": true, "channel": form.Get("channel"), "ts": form.Get("ts")})
	})
	fake.server = httptest.NewServer(mux)
	t.Cleanup(fake.server.Close)
	return fake
}

func (s *fakeSlack) record(r *http.Request, method string) url.Values {
	_ = r.ParseForm()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, slackCall{Method: method, Form: r.PostForm})
	return r.PostForm
}

func (s *fakeSlack) mapUser(email, slackUserID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users[email] = slackUserID
}

func (s *fakeSlack) callsTo(method string) []slackCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []slackCall{}
	for _, call := range s.calls {
		if call.Method == method {
			out = append(out, call)
		}
	}
	return out
}

func writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}
