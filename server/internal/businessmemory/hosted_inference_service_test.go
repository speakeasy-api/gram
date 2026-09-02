package businessmemory

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/business_memories"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/killswitches/hostedinference"
	"github.com/speakeasy-api/gram/server/internal/killswitches/mcptoolexecution"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

const businessMemoryDenialNote = "Hosted inference is paused for this account."

type businessMemoryEvaluator struct {
	result killswitches.EvaluationResult
	calls  int
}

func (e *businessMemoryEvaluator) Evaluate(context.Context, killswitches.EvaluationRequest) killswitches.EvaluationResult {
	e.calls++
	return e.result
}

type businessMemoryCompletionClient struct {
	checkpoint hostedinference.AttemptCheckpoint
}

func (c *businessMemoryCompletionClient) check(ctx context.Context, organizationID string) error {
	if err := c.checkpoint.Check(ctx, organizationID); err != nil {
		return fmt.Errorf("check hosted inference: %w", err)
	}
	return nil
}

func (c *businessMemoryCompletionClient) GetCompletion(ctx context.Context, request openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	return nil, c.check(ctx, request.OrgID)
}

func (c *businessMemoryCompletionClient) GetCompletionStream(ctx context.Context, request openrouter.CompletionRequest) (openrouter.StreamReader, error) {
	return nil, c.check(ctx, request.OrgID)
}

func (c *businessMemoryCompletionClient) GetObjectCompletion(ctx context.Context, request openrouter.ObjectCompletionRequest) (*openrouter.CompletionResponse, error) {
	return nil, c.check(ctx, request.OrgID)
}

func (c *businessMemoryCompletionClient) CreateEmbeddings(ctx context.Context, orgID string, _ string, _ []string, _ ...openrouter.EmbeddingOption) ([][]float32, error) {
	return nil, c.check(ctx, orgID)
}

func (c *businessMemoryCompletionClient) ResolveKey(context.Context, string, string, billing.ModelUsageSource, openrouter.KeyType) (openrouter.ResolvedKey, error) {
	return openrouter.PlatformKey(), nil
}

func TestSearchBusinessMemoriesPreservesGovernedClassificationThroughAgenticClient(t *testing.T) {
	t.Parallel()

	infra, cleanup, err := testenv.Launch(t.Context(), testenv.LaunchOptions{Postgres: true})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	conn, err := infra.CloneTestDatabase(t, "business_memory_hosted_inference")
	require.NoError(t, err)

	const organizationID = "org_test"
	const userID = "user_test"
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID: organizationID, Name: "Test Organization", Slug: "test-organization", WorkosID: conv.ToPGText(organizationID), Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
	_, err = usersrepo.New(conn).UpsertUser(t.Context(), usersrepo.UpsertUserParams{
		ID: userID, Email: "user@example.invalid", DisplayName: "Test User", PhotoUrl: pgtype.Text{}, Admin: false,
	})
	require.NoError(t, err)
	_, err = orgrepo.New(conn).UpsertOrganizationUserRelationship(t.Context(), orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: organizationID, UserID: conv.ToPGText(userID),
	})
	require.NoError(t, err)
	project, err := projectsrepo.New(conn).CreateProject(t.Context(), projectsrepo.CreateProjectParams{
		Name: "Test Project", Slug: "test-project", OrganizationID: organizationID,
	})
	require.NoError(t, err)

	registry, err := mcptoolexecution.NewRegistry(conn)
	require.NoError(t, err)
	result, err := killswitches.NewMatchResult("0198a1b2-c3d4-7000-8000-0123456789ab", businessMemoryDenialNote)
	require.NoError(t, err)
	evaluator := &businessMemoryEvaluator{result: result}
	checkpoint, err := hostedinference.NewCheckpoint(registry, evaluator, time.Second)
	require.NoError(t, err)
	gate := &businessMemoryCompletionClient{checkpoint: checkpoint}
	logger := testenv.NewLogger(t)
	client := chat.NewAgenticChatClient(logger, nil, nil, nil, gate, nil)
	service := &Service{
		logger: logger, db: conn, completions: client,
		authz: authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient()),
	}
	sessionID := "session_test"
	ctx := contextvalues.WithValidatedGramSession(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: organizationID, UserID: userID, SessionID: &sessionID, ProjectID: &project.ID,
	}, false)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource))

	_, err = service.SearchBusinessMemories(ctx, &gen.SearchBusinessMemoriesPayload{Query: "placeholder query", Limit: 10})
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeAIAccessDenied, shareable.Code)
	require.Equal(t, businessMemoryDenialNote, shareable.Error())
	require.Equal(t, 1, evaluator.calls)
}
