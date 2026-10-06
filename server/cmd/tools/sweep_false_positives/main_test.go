package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// sweepPageTrace records the actual SQLc page limits, so the test proves both
// bounded materialization and complete traversal rather than just final counts.
type sweepPageTrace struct {
	limits []int32
}

func (tr *sweepPageTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(data.SQL, "-- name: ListSweepCandidates ") {
		if limit, ok := data.Args[len(data.Args)-1].(int32); ok {
			tr.limits = append(tr.limits, limit)
		}
	}
	return ctx
}

func (*sweepPageTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestSweepCapsPagesAndProcessesEveryFinding(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "sweep_pages")
	require.NoError(t, err)
	orgID := "org_" + uuid.NewString()
	require.NoError(t, testrepo.New(db).CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{
		ID: orgID, Name: "Sweep test", Slug: "sweep-" + uuid.NewString(), GramAccountType: "free",
		FreeTrialStartedAt: conv.ToPGTimestamptz(time.Now()), FreeTrialEndsAt: conv.ToPGTimestamptz(time.Now().AddDate(0, 0, 14)),
	}))
	project, err := projectsrepo.New(db).CreateProject(ctx, projectsrepo.CreateProjectParams{
		OrganizationID: orgID, Name: "Sweep test", Slug: "sweep-test",
	})
	require.NoError(t, err)
	policy, err := riskrepo.New(db).CreateRiskPolicy(ctx, riskrepo.CreateRiskPolicyParams{
		ID: uuid.Must(uuid.NewV7()), OrganizationID: orgID, ProjectID: project.ID,
		Name: "Sweep test", PolicyType: "standard", Sources: []string{"presidio"},
		CustomRuleIds: []string{}, Enabled: true, Action: "flag", AudienceType: "everyone",
	})
	require.NoError(t, err)
	chatID, err := chatrepo.New(db).UpsertChat(ctx, chatrepo.UpsertChatParams{
		ID: uuid.Must(uuid.NewV7()), OrganizationID: orgID, ProjectID: project.ID,
	})
	require.NoError(t, err)
	messageID, err := testrepo.New(db).InsertChatMessage(ctx, testrepo.InsertChatMessageParams{
		ChatID: chatID, ProjectID: conv.ToNullUUID(project.ID), Role: "user", Content: "noreply@example.com",
	})
	require.NoError(t, err)

	const count = defaultBatchSize + 1
	fixtures := make([]riskrepo.InsertRiskResultsParams, count)
	for i := range fixtures {
		fixtures[i] = riskrepo.InsertRiskResultsParams{
			ID: uuid.Must(uuid.NewV7()), OrganizationID: orgID, ProjectID: project.ID,
			RiskPolicyID: policy.ID, RiskPolicyVersion: 1,
			ChatMessageID: conv.ToNullUUID(messageID), Source: "presidio", Found: true,
			RuleID: conv.ToPGText("pii.email_address"), Match: conv.ToPGText("noreply@example.com"),
		}
	}
	n, err := riskrepo.New(db).InsertRiskResults(ctx, fixtures)
	require.NoError(t, err)
	require.EqualValues(t, count, n)

	tracer := &sweepPageTrace{}
	poolConfig := db.Config()
	poolConfig.ConnConfig.Tracer = tracer
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	report, err := sweep(ctx, pool, config{
		orgID: orgID, projectID: project.ID, to: time.Now().Add(time.Hour),
		batchSize: maxBatchSize, dryRun: false,
	})
	require.NoError(t, err)
	require.EqualValues(t, count, report.scanned)
	require.EqualValues(t, count, report.flagged)
	require.EqualValues(t, count, report.updated)
	require.Equal(t, fixtures[count-1].ID, report.lastCursor)
	require.Equal(t, []int32{defaultBatchSize, defaultBatchSize}, tracer.limits)
	marked, err := riskrepo.New(db).CountFalsePositiveRiskResults(ctx, project.ID)
	require.NoError(t, err)
	require.EqualValues(t, count, marked)
}
