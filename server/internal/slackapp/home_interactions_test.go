package slackapp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/feature"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/risk/approval"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections"
	slackrepo "github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

const (
	homeTestOrg      = "org-slackapp-home"
	homeTestApprover = "home-approver"
)

type allFlags struct{}

func (allFlags) IsFlagEnabled(context.Context, feature.Flag, string, map[string]string) (bool, error) {
	return true, nil
}

type homeFixture struct {
	db         *pgxpool.Pool
	projectID  uuid.UUID
	approvals  *approval.Service
	approvalID uuid.UUID
}

// newHomeFixture seeds an organization with one project, an approver who is
// a plain member, and one open tool approval request the approver can decide.
func newHomeFixture(t *testing.T) homeFixture {
	t.Helper()
	ctx := t.Context()
	db, err := slackappInfra.CloneTestDatabase(t, "slackapp_home")
	require.NoError(t, err)
	_, err = orgrepo.New(db).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{ID: homeTestOrg, Name: "Home tests", Slug: "home-tests", WorkosID: pgtype.Text{}, Whitelisted: pgtype.Bool{}, CreationSource: pgtype.Text{}})
	require.NoError(t, err)
	project, err := projectsrepo.New(db).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Home project", Slug: "home-project", OrganizationID: homeTestOrg})
	require.NoError(t, err)
	_, err = usersrepo.New(db).UpsertUser(ctx, usersrepo.UpsertUserParams{ID: homeTestApprover, Email: "home-approver@example.invalid", DisplayName: "Home approver", PhotoUrl: pgtype.Text{}, Admin: false})
	require.NoError(t, err)
	_, err = orgrepo.New(db).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{OrganizationID: homeTestOrg, UserID: pgtype.Text{String: homeTestApprover, Valid: true}})
	require.NoError(t, err)

	approvalConfig, err := policycore.MarshalApprovalConfig(&policycore.ApprovalConfig{Approvers: []string{urn.NewPrincipal(urn.PrincipalTypeUser, homeTestApprover).String()}})
	require.NoError(t, err)
	scope, err := json.Marshal(policycore.MCPScope{AllServers: true, ToolAnnotations: nil, Servers: []policycore.MCPServerScope{}})
	require.NoError(t, err)
	policyRow, err := riskrepo.New(db).CreateRiskPolicy(ctx, riskrepo.CreateRiskPolicyParams{
		ID: uuid.New(), ProjectID: project.ID, OrganizationID: homeTestOrg, Name: "Approve writes", PolicyType: "standard",
		Sources: []string{}, PresidioEntities: []string{}, AnalyzerConfig: nil, PromptInjectionRules: []string{}, DisabledRules: []string{}, CustomRuleIds: []string{},
		McpScope: scope, Enabled: true, Action: policycore.ActionRequireApproval, AudienceType: "everyone", ShadowMcpDisposition: pgtype.Text{}, AutoName: false,
		UserMessage: pgtype.Text{}, Prompt: pgtype.Text{}, ModelConfig: nil, ApprovalConfig: approvalConfig, Score: pgtype.Float8{},
	})
	require.NoError(t, err)

	serverURL, err := url.Parse("https://speakeasy.test")
	require.NoError(t, err)
	approvals := approval.NewService(testenv.NewLogger(t), db, serverURL, audit.NewLogger())
	agentURN := urn.NewPrincipal(urn.PrincipalTypeAgent, uuid.NewString()).String()
	outcome, err := approvals.HoldCall(ctx, approval.HoldInput{
		Policies: []policycore.Policy{policycore.Project(policyRow, nil)}, OrganizationID: homeTestOrg, ProjectID: project.ID,
		RequesterURN: agentURN, RequesterUserID: "", Surface: "hosted_mcp", ToolsetID: "", ServerID: uuid.NewString(),
		ToolName: "create_issue", ChatID: "", Arguments: []byte(`{"title":"bug"}`),
	})
	require.NoError(t, err)
	require.False(t, outcome.Allowed)
	row, status, err := approvals.Request(ctx, approval.RequestInput{Token: outcome.Token, RequesterURN: agentURN, OrganizationID: homeTestOrg, Reason: "File the bug.", Scope: "", ExpiresIn: 0})
	require.NoError(t, err)
	require.Equal(t, approval.StatusPending, status)
	return homeFixture{db: db, projectID: project.ID, approvals: approvals, approvalID: row.ID}
}

func (f homeFixture) events(t *testing.T, poster *fakePoster) *Events {
	t.Helper()
	router := &fakeRouter{workspaces: []slackdirectoryconnections.RoutingWorkspace{{ConnectionID: uuid.New(), OrganizationID: homeTestOrg, TeamID: "T1", ProjectID: f.projectID, Token: "xoxb-test"}}}
	events := NewEvents(testenv.NewLogger(t), f.db, router, &fakeDispatcher{}, poster, testSecret, nil).
		WithDashboardURL("https://app.example.invalid").
		WithHome(NewHomeSource(testenv.NewLogger(t), f.db, allFlags{}))
	events.resolveSender = func(_ context.Context, params slackrepo.ResolveSlackMappingUserParams) (string, error) {
		if params.OrganizationID != homeTestOrg || params.SlackUserID != "U1" {
			return "", pgx.ErrNoRows
		}
		return homeTestApprover, nil
	}
	return events
}

func (f homeFixture) status(t *testing.T) string {
	t.Helper()
	row, err := f.approvals.Get(t.Context(), homeTestOrg, f.projectID, f.approvalID)
	require.NoError(t, err)
	return row.Status
}

func homeClickRequest(t *testing.T, actionID, value, organizationID string) *http.Request {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"type":    "block_actions",
		"team":    map[string]any{"id": "T1"},
		"user":    map[string]any{"id": "U1"},
		"view":    map[string]any{"type": "home", "private_metadata": organizationID},
		"actions": []any{map[string]any{"action_id": actionID, "value": value}},
	})
	require.NoError(t, err)
	return signedRequest(t, url.Values{"payload": {string(payload)}}.Encode(), testSecret)
}

type recordingHandler struct{ bodies []string }

func (h *recordingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	h.bodies = append(h.bodies, string(body))
	w.WriteHeader(http.StatusOK)
}

func TestHomeLoadShowsOnlyPermittedSections(t *testing.T) {
	t.Parallel()
	f := newHomeFixture(t)
	source := NewHomeSource(testenv.NewLogger(t), f.db, allFlags{})

	data := source.Load(t.Context(), homeTestOrg, homeTestApprover, "https://app.example.invalid")
	require.True(t, data.Linked)
	require.Equal(t, "Home tests", data.OrganizationName)
	require.True(t, data.ApprovalsEnabled)
	require.Len(t, data.Approvals, 1)
	require.Equal(t, f.approvalID.String(), data.Approvals[0].ID)
	require.Equal(t, f.projectID.String(), data.Approvals[0].ProjectID)
	require.True(t, data.Approvals[0].CanDecide)
	require.Contains(t, data.Approvals[0].URL, "/home-tests/projects/home-project/risk-policies?")
	// A plain member is not an org admin, so risk and MCP review stay hidden.
	require.False(t, data.RiskEnabled)
	require.False(t, data.MCPRequestsEnabled)
	require.False(t, data.ApprovalsError || data.ServersError)

	selectors, err := authz.NewSelector(authz.ScopeOrgAdmin, homeTestOrg).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(f.db).UpsertPrincipalGrant(t.Context(), accessrepo.UpsertPrincipalGrantParams{OrganizationID: homeTestOrg, PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, homeTestApprover), Scope: string(authz.ScopeOrgAdmin), Selectors: selectors})
	require.NoError(t, err)
	data = source.Load(t.Context(), homeTestOrg, homeTestApprover, "https://app.example.invalid")
	require.True(t, data.RiskEnabled)
	require.True(t, data.MCPRequestsEnabled)
	require.False(t, data.RiskError || data.MCPRequestsError)
}

func TestHomeLoadHidesRequestsFromNonApprovers(t *testing.T) {
	t.Parallel()
	f := newHomeFixture(t)
	_, err := usersrepo.New(f.db).UpsertUser(t.Context(), usersrepo.UpsertUserParams{ID: "home-other", Email: "home-other@example.invalid", DisplayName: "Other", PhotoUrl: pgtype.Text{}, Admin: false})
	require.NoError(t, err)
	_, err = orgrepo.New(f.db).UpsertOrganizationUserRelationship(t.Context(), orgrepo.UpsertOrganizationUserRelationshipParams{OrganizationID: homeTestOrg, UserID: pgtype.Text{String: "home-other", Valid: true}})
	require.NoError(t, err)

	data := NewHomeSource(testenv.NewLogger(t), f.db, allFlags{}).Load(t.Context(), homeTestOrg, "home-other", "https://app.example.invalid")
	require.False(t, data.ApprovalsEnabled)
	require.Empty(t, data.Approvals)
}

func TestHomeClickDecidesAndRepublishes(t *testing.T) {
	t.Parallel()
	f := newHomeFixture(t)
	poster := &fakePoster{}
	next := &recordingHandler{}
	handler := NewHomeInteractions(f.events(t, poster), f.approvals, next)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, homeClickRequest(t, homeClickApprove, f.projectID.String()+":"+f.approvalID.String(), homeTestOrg))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Empty(t, next.bodies)
	require.Equal(t, string(approval.StatusApproved), f.status(t))
	require.Len(t, poster.calls, 1)
	require.Equal(t, "views.publish", poster.calls[0].method)
	require.Equal(t, "U1", poster.calls[0].payload["user_id"])
	rawView, ok := poster.calls[0].payload["view"].(string)
	require.True(t, ok)
	var view map[string]any
	require.NoError(t, json.Unmarshal([]byte(rawView), &view))
	require.Equal(t, homeTestOrg, view["private_metadata"])
}

func TestHomeClickPinnedToAnotherOrganizationDoesNotDecide(t *testing.T) {
	t.Parallel()
	f := newHomeFixture(t)
	poster := &fakePoster{}
	handler := NewHomeInteractions(f.events(t, poster), f.approvals, &recordingHandler{})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, homeClickRequest(t, homeClickApprove, f.projectID.String()+":"+f.approvalID.String(), "org-someone-else"))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, string(approval.StatusPending), f.status(t))
	// The clicker still gets their own Home back, with the refusal.
	require.Len(t, poster.calls, 1)
}

func TestHomeInteractionsPassOtherClicksThrough(t *testing.T) {
	t.Parallel()
	next := &recordingHandler{}
	events := newTestEvents(t, &fakeRouter{}, &fakeDispatcher{}, &fakePoster{}, "")
	handler := NewHomeInteractions(events, nil, next)

	body := url.Values{"payload": {`{"type":"block_actions","container":{"type":"message"},"actions":[{"action_id":"approval_approve","value":"x"}]}`}}.Encode()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, signedRequest(t, body, testSecret))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, []string{body}, next.bodies)
}

func TestHomeInteractionsRejectUnsignedHomeClick(t *testing.T) {
	t.Parallel()
	next := &recordingHandler{}
	poster := &fakePoster{}
	events := newTestEvents(t, &fakeRouter{}, &fakeDispatcher{}, poster, "")
	handler := NewHomeInteractions(events, nil, next)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, homeClickRequest(t, homeClickDeny, uuid.NewString()+":"+uuid.NewString(), homeTestOrg))
	require.Equal(t, http.StatusOK, rec.Code) // signed with the right secret

	req := homeClickRequest(t, homeClickDeny, uuid.NewString()+":"+uuid.NewString(), homeTestOrg)
	req.Header.Set("X-Slack-Signature", "v0=bad")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Empty(t, next.bodies)
	require.Empty(t, poster.calls)
}
