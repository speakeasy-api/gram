package slackapproval_test

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
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orghost"
	"github.com/speakeasy-api/gram/server/internal/risk/approval"
	"github.com/speakeasy-api/gram/server/internal/risk/approval/repo"
	"github.com/speakeasy-api/gram/server/internal/risk/approval/slackapproval"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections"
	slackrepo "github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	slackapi "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/api"
	"github.com/speakeasy-api/gram/server/internal/urn"
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

// fixture is one project with a require_approval policy, an agent that calls
// through it, and a fake Slack workspace.
type fixture struct {
	db             *pgxpool.Pool
	approvals      *approval.Service
	cards          *slackapproval.Cards
	notifier       *slackapproval.Notifier
	slack          *fakeSlack
	workspaces     *fakeWorkspaces
	organizationID string
	projectID      uuid.UUID
	// approverID is the signed-in test user, the policy's first approver.
	approverID string
	agentURN   string
	// serverID is the MCP server, Team Slack, that held calls go to.
	serverID uuid.UUID
	// connection is the fake workspace's connection, once connectWorkspace
	// records it.
	connection *slackrepo.SlackDirectoryConnection
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
	approvals := approval.NewService(logger, conn, serverURL, audit.NewLogger())
	cards := slackapproval.NewCards(conn, approvals, orghost.New(orghost.Config{ServerURL: serverURL, SiteURL: siteURL, PlatformHosts: nil, LegacyDefaultHost: nil, NewOrganizationDefaultHost: nil}))
	slack := newFakeSlack(t)
	workspaces := &fakeWorkspaces{workspaces: []slackdirectoryconnections.MessagingWorkspace{{ConnectionID: uuid.New(), TeamID: testTeamID, Token: testBotToken}}}
	// The MCP server held calls go to, so cards can name it.
	toolsetID, err := testrepo.New(conn).CreateToolsetFixture(ctx, testrepo.CreateToolsetFixtureParams{
		ID:             uuid.New(),
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Name:           "Team Slack tools",
		Slug:           "team-slack-tools-" + uuid.NewString()[:8],
	})
	require.NoError(t, err)
	server, err := mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                    uuid.New(),
		ProjectID:             *authCtx.ProjectID,
		Name:                  conv.ToPGText("Team Slack"),
		Slug:                  conv.ToPGText("team-slack-" + uuid.NewString()[:8]),
		EnvironmentID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserSessionIssuerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RemoteMcpServerID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		TunneledMcpServerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID:             uuid.NullUUID{UUID: toolsetID, Valid: true},
		UnproxiedMcpServerID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolVariationsGroupID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:            "private",
		NetworkAccessMode:     pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	return &fixture{
		db:             conn,
		approvals:      approvals,
		cards:          cards,
		notifier:       slackapproval.NewNotifier(logger, cards, workspaces, slackapi.NewClient(slack.server.URL, slack.server.Client()), true),
		slack:          slack,
		workspaces:     workspaces,
		organizationID: authCtx.ActiveOrganizationID,
		projectID:      *authCtx.ProjectID,
		approverID:     authCtx.UserID,
		agentURN:       urn.NewPrincipal(urn.PrincipalTypeAgent, uuid.NewString()).String(),
		serverID:       server.ID,
	}
}

// addPolicy creates a require_approval policy over every server with the
// given approvers.
func (f *fixture) addPolicy(t *testing.T, approvers ...string) policycore.Policy {
	t.Helper()
	approvalConfig, err := policycore.MarshalApprovalConfig(&policycore.ApprovalConfig{Approvers: approvers})
	require.NoError(t, err)
	scope, err := json.Marshal(policycore.MCPScope{AllServers: true, ToolAnnotations: nil, Servers: []policycore.MCPServerScope{}})
	require.NoError(t, err)
	row, err := riskrepo.New(f.db).CreateRiskPolicy(t.Context(), riskrepo.CreateRiskPolicyParams{
		ID:                   uuid.New(),
		ProjectID:            f.projectID,
		OrganizationID:       f.organizationID,
		Name:                 "Approve Slack posts",
		PolicyType:           "standard",
		Sources:              []string{},
		PresidioEntities:     []string{},
		AnalyzerConfig:       nil,
		PromptInjectionRules: []string{},
		DisabledRules:        []string{},
		CustomRuleIds:        []string{},
		McpScope:             scope,
		Enabled:              true,
		Action:               policycore.ActionRequireApproval,
		AudienceType:         "everyone",
		ShadowMcpDisposition: pgtype.Text{String: "", Valid: false},
		AutoName:             false,
		UserMessage:          pgtype.Text{String: "", Valid: false},
		Prompt:               pgtype.Text{String: "", Valid: false},
		ModelConfig:          nil,
		ApprovalConfig:       approvalConfig,
		Score:                pgtype.Float8{Float64: 0, Valid: false},
	})
	require.NoError(t, err)
	return policycore.Project(row, nil)
}

// setApprovers replaces a policy's approver list.
func (f *fixture) setApprovers(t *testing.T, policyID uuid.UUID, approvers ...string) {
	t.Helper()
	approvalConfig, err := policycore.MarshalApprovalConfig(&policycore.ApprovalConfig{Approvers: approvers})
	require.NoError(t, err)
	_, err = f.db.Exec(t.Context(), "UPDATE risk_policies SET approval_config = $1 WHERE id = $2", approvalConfig, policyID) //nolint:glint // notestingrawsql: no query edits only the approver list of a policy
	require.NoError(t, err)
}

// pendingRequest holds a call under policy and files its request, as an
// agent would. requesterUserID is set for a person calling through their own
// session.
func (f *fixture) pendingRequest(t *testing.T, policy policycore.Policy, requesterUserID string) uuid.UUID {
	t.Helper()
	requesterURN := f.agentURN
	if requesterUserID != "" {
		requesterURN = urn.NewPrincipal(urn.PrincipalTypeUser, requesterUserID).String()
	}
	outcome, err := f.approvals.HoldCall(t.Context(), approval.HoldInput{
		Policies:        []policycore.Policy{policy},
		OrganizationID:  f.organizationID,
		ProjectID:       f.projectID,
		RequesterURN:    requesterURN,
		RequesterUserID: requesterUserID,
		Surface:         "hosted_mcp",
		ToolsetID:       "",
		ServerID:        f.serverID.String(),
		ToolName:        "slack__post_message",
		ChatID:          "",
		Arguments:       []byte(`{"channel":"C123","text":"Deploy <!channel> finished"}`),
	})
	require.NoError(t, err)
	require.False(t, outcome.Allowed)
	row, status, err := f.approvals.Request(t.Context(), approval.RequestInput{
		Token:          outcome.Token,
		RequesterURN:   requesterURN,
		OrganizationID: f.organizationID,
		Reason:         "Tell the team the deploy finished.",
		Scope:          "",
		ExpiresIn:      0,
	})
	require.NoError(t, err)
	require.Equal(t, approval.StatusPending, status)
	return row.ID
}

func (f *fixture) target(approvalID uuid.UUID) slackapproval.Target {
	return slackapproval.Target{OrganizationID: f.organizationID, ProjectID: f.projectID, ApprovalID: approvalID}
}

// addMember adds an organization member and returns their user ID and email.
func (f *fixture) addMember(t *testing.T, name string) (string, string) {
	t.Helper()
	userID := "user_" + uuid.NewString()
	email := userID + "@example.com"
	_, err := userrepo.New(f.db).UpsertUser(t.Context(), userrepo.UpsertUserParams{
		ID:          userID,
		Email:       email,
		DisplayName: name,
		PhotoUrl:    conv.PtrToPGText(nil),
		Admin:       false,
	})
	require.NoError(t, err)
	_, err = orgrepo.New(f.db).UpsertOrganizationUserRelationship(t.Context(), orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: f.organizationID,
		UserID:         conv.ToPGText(userID),
	})
	require.NoError(t, err)
	return userID, email
}

// addRole creates an organization role, assigns it to members, and returns
// its principal URN.
func (f *fixture) addRole(t *testing.T, slug string, members ...string) string {
	t.Helper()
	ctx := t.Context()
	now := time.Now().UTC()
	role, err := accessrepo.New(f.db).CreateOrganizationRole(ctx, accessrepo.CreateOrganizationRoleParams{
		OrganizationID:    f.organizationID,
		WorkosSlug:        slug,
		WorkosName:        slug,
		WorkosDescription: conv.ToPGTextEmpty(""),
		WorkosCreatedAt:   conv.ToPGTimestamptz(now),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(now),
		WorkosLastEventID: conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	for _, member := range members {
		_, err := accessrepo.New(f.db).UpsertOrganizationRoleAssignment(ctx, accessrepo.UpsertOrganizationRoleAssignmentParams{
			OrganizationID:     f.organizationID,
			WorkosUserID:       "workos_" + member,
			UserID:             conv.ToPGText(member),
			WorkosMembershipID: conv.ToPGTextEmpty(""),
			WorkosUpdatedAt:    conv.ToPGTimestamptz(now),
			WorkosLastEventID:  conv.ToPGTextEmpty(""),
			WorkosRoleSlug:     slug,
		})
		require.NoError(t, err)
	}
	return role.RoleUrn
}

// connectWorkspace records the fake workspace as connected to the
// organization, the first time it is called.
func (f *fixture) connectWorkspace(t *testing.T) slackrepo.SlackDirectoryConnection {
	t.Helper()
	if f.connection != nil {
		return *f.connection
	}
	connection, err := slackrepo.New(f.db).CreateSlackDirectoryConnection(t.Context(), slackrepo.CreateSlackDirectoryConnectionParams{
		OrganizationID: f.organizationID, SlackTeamID: testTeamID, SlackTeamName: conv.ToPGText("Example workspace"),
		CredentialsEncrypted: conv.ToPGTextEmpty(""), GrantedScopes: []string{}, Generation: uuid.New(),
	})
	require.NoError(t, err)
	f.connection = &connection
	return connection
}

// mapToSlack confirms userID as slackUserID in the fake workspace, the way an
// admin maps people on the Slack workspaces page.
func (f *fixture) mapToSlack(t *testing.T, userID, slackUserID string) {
	t.Helper()
	ctx := t.Context()
	queries := slackrepo.New(f.db)
	connection := f.connectWorkspace(t)
	published := conv.ToPGTimestamptz(time.Now())
	_, err := queries.PublishSlackDirectorySync(ctx, slackrepo.PublishSlackDirectorySyncParams{
		OrganizationID: f.organizationID, ID: connection.ID, Generation: connection.Generation, PublishedAt: published,
	})
	require.NoError(t, err)
	require.NoError(t, queries.UpsertSlackDirectoryMembershipBatch(ctx, slackrepo.UpsertSlackDirectoryMembershipBatchParams{
		OrganizationID: f.organizationID, SlackTeamID: testTeamID, LastSeenAt: published,
		UserIds: []string{slackUserID}, DisplayNames: []string{"Mapped person"}, Emails: []string{"mapped@example.com"},
		Statuses: []string{"active"}, MemberTypes: []string{"person"}, ProviderUpdatedAts: []pgtype.Timestamptz{{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}},
	}))
	require.NoError(t, queries.ConfirmSlackIdentityMapping(ctx, slackrepo.ConfirmSlackIdentityMappingParams{
		OrganizationID: f.organizationID, SlackTeamID: testTeamID, SlackUserID: slackUserID, UserID: userID,
	}))
}

// email returns a member's email.
func (f *fixture) email(t *testing.T, userID string) string {
	t.Helper()
	user, err := userrepo.New(f.db).GetUser(t.Context(), userID)
	require.NoError(t, err)
	return user.Email
}

// messages lists the DMs recorded for a request.
func (f *fixture) messages(t *testing.T, approvalID uuid.UUID) []repo.RiskPolicyApprovalMessage {
	t.Helper()
	rows, err := repo.New(f.db).ListApprovalMessages(t.Context(), repo.ListApprovalMessagesParams{OrganizationID: f.organizationID, ProjectID: f.projectID, ApprovalID: approvalID})
	require.NoError(t, err)
	return rows
}

// fakeWorkspaces is a fixed list of workspaces the app can post to, and the
// error for workspaces that cannot be used right now.
type fakeWorkspaces struct {
	workspaces  []slackdirectoryconnections.MessagingWorkspace
	unavailable error
}

func (w *fakeWorkspaces) MessagingWorkspaces(context.Context, string, string) ([]slackdirectoryconnections.MessagingWorkspace, error) {
	return w.workspaces, w.unavailable
}

// slackCall is one Slack Web API call the fake received.
type slackCall struct {
	Method string
	Token  string
	Form   url.Values
}

// fakeSlack serves the Slack Web API methods the notifier calls.
type fakeSlack struct {
	server *httptest.Server

	mu    sync.Mutex
	calls []slackCall
	// users maps emails to Slack user IDs for users.lookupByEmail.
	users map[string]string
	// unconfirmed are emails users.lookupByEmail reports as not confirmed.
	unconfirmed map[string]bool
	// failPost maps a Slack user ID to the error chat.postMessage answers.
	failPost map[string]string
	nextTS   int
}

func newFakeSlack(t *testing.T) *fakeSlack {
	t.Helper()
	fake := &fakeSlack{server: nil, mu: sync.Mutex{}, calls: nil, users: map[string]string{}, unconfirmed: map[string]bool{}, failPost: map[string]string{}, nextTS: 0}
	mux := http.NewServeMux()
	mux.HandleFunc("/users.lookupByEmail", func(w http.ResponseWriter, r *http.Request) {
		form := fake.record(r, "users.lookupByEmail")
		fake.mu.Lock()
		id, ok := fake.users[form.Get("email")]
		confirmed := !fake.unconfirmed[form.Get("email")]
		fake.mu.Unlock()
		if !ok {
			writeJSON(w, map[string]any{"ok": false, "error": "users_not_found"})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "user": map[string]any{"id": id, "is_email_confirmed": confirmed}})
	})
	mux.HandleFunc("/chat.postMessage", func(w http.ResponseWriter, r *http.Request) {
		form := fake.record(r, "chat.postMessage")
		fake.mu.Lock()
		failure := fake.failPost[form.Get("channel")]
		fake.nextTS++
		ts := fmt.Sprintf("1700000000.%06d", fake.nextTS)
		fake.mu.Unlock()
		if failure != "" {
			writeJSON(w, map[string]any{"ok": false, "error": failure})
			return
		}
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
	s.calls = append(s.calls, slackCall{Method: method, Token: r.Header.Get("Authorization"), Form: r.PostForm})
	return r.PostForm
}

// mapUser makes users.lookupByEmail find email as slackUserID.
func (s *fakeSlack) mapUser(email, slackUserID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users[email] = slackUserID
}

// mapUnconfirmedUser makes users.lookupByEmail find email as slackUserID, with
// an email Slack has not confirmed.
func (s *fakeSlack) mapUnconfirmedUser(email, slackUserID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users[email] = slackUserID
	s.unconfirmed[email] = true
}

// failPostTo makes chat.postMessage to slackUserID answer with code.
func (s *fakeSlack) failPostTo(slackUserID, code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failPost[slackUserID] = code
}

// callsTo lists the calls to one method.
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
