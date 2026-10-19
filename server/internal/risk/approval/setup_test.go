package approval

import (
	"context"
	"encoding/json"
	"log"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpriskscan"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/risk/approval/repo"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	userrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
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

// fixture is one project with a require_approval policy whose only approver
// is the signed-in test user.
type fixture struct {
	service        *Service
	db             *pgxpool.Pool
	organizationID string
	projectID      uuid.UUID
	approverID     string
	policy         policycore.Policy
	agentURN       string
}

func newFixture(t *testing.T) fixture {
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

	approvalConfig, err := policycore.MarshalApprovalConfig(&policycore.ApprovalConfig{Approvers: []string{urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID).String()}})
	require.NoError(t, err)
	scope, err := json.Marshal(policycore.MCPScope{AllServers: true, Servers: []policycore.MCPServerScope{}})
	require.NoError(t, err)
	row, err := riskrepo.New(conn).CreateRiskPolicy(ctx, riskrepo.CreateRiskPolicyParams{
		ID:                   uuid.New(),
		ProjectID:            *authCtx.ProjectID,
		OrganizationID:       authCtx.ActiveOrganizationID,
		Name:                 "Approve writes",
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

	serverURL, err := url.Parse("https://speakeasy.test")
	require.NoError(t, err)
	return fixture{
		service:        NewService(logger, conn, serverURL),
		db:             conn,
		organizationID: authCtx.ActiveOrganizationID,
		projectID:      *authCtx.ProjectID,
		approverID:     authCtx.UserID,
		policy:         policycore.Project(row, nil),
		agentURN:       urn.NewPrincipal(urn.PrincipalTypeAgent, uuid.NewString()).String(),
	}
}

// call is a held call by the fixture's agent.
func (f fixture) call(arguments string) mcpriskscan.ApprovalCall {
	return mcpriskscan.ApprovalCall{
		Policy:          f.policy,
		OrganizationID:  f.organizationID,
		ProjectID:       f.projectID,
		RequesterURN:    f.agentURN,
		RequesterUserID: "",
		Surface:         mcpriskscan.SurfaceHostedMCP,
		ToolsetID:       "",
		ServerID:        uuid.NewSHA1(uuid.Nil, []byte("server")).String(),
		ToolName:        "create_issue",
		ChatID:          "",
		Arguments:       []byte(arguments),
	}
}

// hold holds a call and returns its token.
func (f fixture) hold(t *testing.T, call mcpriskscan.ApprovalCall) string {
	t.Helper()
	outcome, err := f.service.HoldCall(t.Context(), call)
	require.NoError(t, err)
	require.False(t, outcome.Allowed)
	require.True(t, IsToken(outcome.Token))
	return outcome.Token
}

// request files the request for a held call by the fixture's agent.
func (f fixture) request(t *testing.T, token string) Status {
	t.Helper()
	_, status, err := f.service.Request(t.Context(), RequestInput{
		Token:          token,
		RequesterURN:   f.agentURN,
		OrganizationID: f.organizationID,
		Reason:         "File the bug the user reported.",
		Scope:          "",
		ExpiresIn:      0,
	})
	require.NoError(t, err)
	return status
}

// approvalID resolves a token to its request id.
func (f fixture) approvalID(t *testing.T, token string) uuid.UUID {
	t.Helper()
	row, err := repo.New(f.db).GetApprovalByToken(t.Context(), token)
	require.NoError(t, err)
	return row.ID
}

// addMember adds a second organization member who is not an approver.
func (f fixture) addMember(t *testing.T) string {
	t.Helper()
	userID := "user_" + uuid.NewString()
	_, err := userrepo.New(f.db).UpsertUser(t.Context(), userrepo.UpsertUserParams{
		ID:          userID,
		Email:       userID + "@example.com",
		DisplayName: "Other User",
		PhotoUrl:    conv.PtrToPGText(nil),
		Admin:       false,
	})
	require.NoError(t, err)
	_, err = orgrepo.New(f.db).UpsertOrganizationUserRelationship(t.Context(), orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: f.organizationID,
		UserID:         conv.ToPGText(userID),
	})
	require.NoError(t, err)
	return userID
}

// shiftClock makes the service's clock run offset from real time.
func (f fixture) shiftClock(offset time.Duration) {
	f.service.now = func() time.Time { return time.Now().Add(offset) }
}
