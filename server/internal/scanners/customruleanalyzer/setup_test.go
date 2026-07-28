package customruleanalyzer_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
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

func cloneDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	return conn
}

type seededProject struct {
	orgID     string
	projectID uuid.UUID
}

func seedProject(t *testing.T, conn *pgxpool.Pool) seededProject {
	t.Helper()
	ctx := t.Context()

	orgID := "test-org-" + uuid.NewString()[:8]
	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        orgID,
		Slug:        orgID,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "test-project",
		Slug:           "test-" + uuid.NewString()[:8],
		OrganizationID: orgID,
	})
	require.NoError(t, err)

	return seededProject{orgID: orgID, projectID: project.ID}
}

func seedCustomRule(t *testing.T, conn *pgxpool.Pool, p seededProject, ruleID, detectionExpr string) {
	t.Helper()
	_, err := riskrepo.New(conn).CreateCustomDetectionRule(t.Context(), riskrepo.CreateCustomDetectionRuleParams{
		ProjectID:      p.projectID,
		OrganizationID: p.orgID,
		RuleID:         ruleID,
		Title:          "test rule",
		Description:    "test rule description",
		DetectionExpr:  pgtype.Text{String: detectionExpr, Valid: true},
		Severity:       "medium",
	})
	require.NoError(t, err)
}

// capturingPub records every Finding handed to Publish so tests can assert on
// the published payloads.
func capturingPub(t *testing.T) (*gcp.MockPublisher[*riskv1.Finding], *[]*riskv1.Finding) {
	t.Helper()
	pub := gcp.NewMockPublisher[*riskv1.Finding]()
	var published []*riskv1.Finding
	pub.On("Publish", mock.Anything, mock.Anything).
		Return(gcp.NewSuccessPublishResult()).
		Run(func(args mock.Arguments) {
			f, ok := args.Get(1).(*riskv1.Finding)
			require.True(t, ok)
			published = append(published, f)
		})
	return pub, &published
}

func newRequest(p seededProject, content string, ruleIDs ...string) *riskv1.CustomRulesAnalysis {
	return riskv1.CustomRulesAnalysis_builder{
		RequestId:         new("req-1"),
		ChatMessageId:     new("msg-1"),
		ProjectId:         new(p.projectID.String()),
		OrganizationId:    new(p.orgID),
		RiskPolicyId:      new("policy-1"),
		RiskPolicyVersion: new(int64(3)),
		CreatedAt:         new("2026-06-20T00:00:00Z"),
		Content:           &content,
		Kind:              new("user_message"),
		CustomRuleIds:     ruleIDs,
	}.Build()
}
