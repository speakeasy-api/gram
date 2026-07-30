package riskfindingscols

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/pipeline"
	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{
		Postgres:   true,
		Redis:      false,
		ClickHouse: true,
		Temporal:   false,
		Presidio:   false,
	})
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

// tenant is a seeded org/project/policy triple in a per-test Postgres clone,
// plus helpers to hang chats, messages, assistants, and risk results off it.
type tenant struct {
	pool      *pgxpool.Pool
	orgID     string
	projectID uuid.UUID
	policyID  uuid.UUID
}

func seedTenant(t *testing.T) *tenant {
	t.Helper()
	ctx := t.Context()

	pool, err := infra.CloneTestDatabase(t, "riskfindingscols")
	require.NoError(t, err)

	orgID := "org_" + uuid.NewString()
	now := time.Now().UTC()
	require.NoError(t, testrepo.New(pool).CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{
		ID:                 orgID,
		Name:               "backfill test org",
		Slug:               "backfill-" + uuid.NewString()[:8],
		GramAccountType:    "free",
		Whitelisted:        true,
		FreeTrialStartedAt: pgtype.Timestamptz{Time: now, Valid: true},
		FreeTrialEndsAt:    pgtype.Timestamptz{Time: now.AddDate(0, 0, 14), Valid: true},
	}))

	project, err := projectsrepo.New(pool).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "backfill",
		Slug:           "backfill",
		OrganizationID: orgID,
	})
	require.NoError(t, err)

	policy, err := riskrepo.New(pool).CreateRiskPolicy(ctx, riskrepo.CreateRiskPolicyParams{
		ID:             uuid.Must(uuid.NewV7()),
		ProjectID:      project.ID,
		OrganizationID: orgID,
		Name:           "backfill policy",
		PolicyType:     "standard",
		Sources:        []string{"presidio"},
		CustomRuleIds:  []string{},
		MessageTypes:   []string{},
		Enabled:        true,
		Action:         "flag",
		AudienceType:   "everyone",
		AutoName:       true,
	})
	require.NoError(t, err)

	return &tenant{
		pool:      pool,
		orgID:     orgID,
		projectID: project.ID,
		policyID:  policy.ID,
	}
}

func (tn *tenant) newChat(t *testing.T) uuid.UUID {
	t.Helper()
	chatID, err := chatrepo.New(tn.pool).UpsertChat(t.Context(), chatrepo.UpsertChatParams{
		ID:             uuid.Must(uuid.NewV7()),
		ProjectID:      tn.projectID,
		OrganizationID: tn.orgID,
	})
	require.NoError(t, err)
	return chatID
}

// newMessage seeds a chat message stamped with createdAt — the event time the
// migration backfills into message_created_at.
func (tn *tenant) newMessage(t *testing.T, chatID uuid.UUID, createdAt time.Time) uuid.UUID {
	t.Helper()
	ctx := t.Context()
	repo := testrepo.New(tn.pool)
	msgID, err := repo.InsertChatMessage(ctx, testrepo.InsertChatMessageParams{
		ChatID:    chatID,
		ProjectID: uuid.NullUUID{UUID: tn.projectID, Valid: true},
		Role:      "user",
		Content:   "hello alice@example.com",
	})
	require.NoError(t, err)
	require.NoError(t, repo.UpdateChatMessageCreatedAt(ctx, testrepo.UpdateChatMessageCreatedAtParams{
		CreatedAt: pgtype.Timestamptz{Time: createdAt, Valid: true},
		ID:        msgID,
	}))
	return msgID
}

// linkAssistant creates an assistant and a live assistant_threads row backing
// chatID, returning both ids so a test can soft-delete the thread.
func (tn *tenant) linkAssistant(t *testing.T, chatID uuid.UUID) (assistantID, threadID uuid.UUID) {
	t.Helper()
	ctx := t.Context()
	repo := assistantsrepo.New(tn.pool)

	assistant, err := repo.CreateAssistant(ctx, assistantsrepo.CreateAssistantParams{
		ProjectID:      tn.projectID,
		OrganizationID: tn.orgID,
		Name:           "backfill assistant " + uuid.NewString()[:8],
		Model:          "test-model",
		Instructions:   "noop",
		WarmTtlSeconds: 300,
		MaxConcurrency: 1,
		Status:         "active",
	})
	require.NoError(t, err)

	threadID, err = repo.UpsertAssistantThread(ctx, assistantsrepo.UpsertAssistantThreadParams{
		AssistantID:   assistant.ID,
		ProjectID:     tn.projectID,
		CorrelationID: uuid.NewString(),
		ChatID:        chatID,
		SourceKind:    "test",
		SourceRefJson: []byte("{}"),
	})
	require.NoError(t, err)

	return assistant.ID, threadID
}

func (tn *tenant) softDeleteThread(t *testing.T, threadID uuid.UUID) {
	t.Helper()
	require.NoError(t, assistantsrepo.New(tn.pool).SoftDeleteAssistantThread(t.Context(), assistantsrepo.SoftDeleteAssistantThreadParams{
		ID:        threadID,
		ProjectID: tn.projectID,
	}))
}

// newFinding seeds a true-positive risk_results row anchored to messageID with
// the given created_at (the scan time) and returns its id.
func (tn *tenant) newFinding(t *testing.T, messageID uuid.UUID, createdAt time.Time) uuid.UUID {
	t.Helper()
	ctx := t.Context()

	id := uuid.Must(uuid.NewV7())
	n, err := riskrepo.New(tn.pool).InsertRiskResults(ctx, []riskrepo.InsertRiskResultsParams{{
		ID:                id,
		ProjectID:         tn.projectID,
		OrganizationID:    tn.orgID,
		RiskPolicyID:      tn.policyID,
		RiskPolicyVersion: 1,
		ChatMessageID:     uuid.NullUUID{UUID: messageID, Valid: true},
		Source:            "presidio",
		Found:             true,
		RuleID:            conv.ToPGText("pii.email_address"),
		Description:       conv.ToPGText("an email"),
		Match:             conv.ToPGText("alice@example.com"),
		Tags:              []string{"pii"},
	}})
	require.NoError(t, err)
	require.EqualValues(t, 1, n)

	require.NoError(t, testrepo.New(tn.pool).UpdateRiskResultCreatedAt(ctx, testrepo.UpdateRiskResultCreatedAtParams{
		CreatedAt: pgtype.Timestamptz{Time: createdAt, Valid: true},
		ID:        id,
	}))
	return id
}

// newNonFinding seeds a risk_results row the source must skip: found=false
// when foundValue is false, or found=true with a NULL rule_id otherwise.
func (tn *tenant) newNonFinding(t *testing.T, messageID uuid.UUID, foundValue bool) uuid.UUID {
	t.Helper()

	id := uuid.Must(uuid.NewV7())
	params := riskrepo.InsertRiskResultsParams{
		ID:                id,
		ProjectID:         tn.projectID,
		OrganizationID:    tn.orgID,
		RiskPolicyID:      tn.policyID,
		RiskPolicyVersion: 1,
		ChatMessageID:     uuid.NullUUID{UUID: messageID, Valid: true},
		Source:            "none",
		Found:             foundValue,
		Tags:              []string{},
	}
	if !foundValue {
		params.RuleID = conv.ToPGText("pii.email_address")
	}
	n, err := riskrepo.New(tn.pool).InsertRiskResults(t.Context(), []riskrepo.InsertRiskResultsParams{params})
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	return id
}

// newContentPartFinding seeds a risk_results row anchored to a chat content
// part instead of a message (chat_message_id IS NULL), so the source's
// chat_messages join misses and message_created_at must fall back to the
// finding's own created_at.
func (tn *tenant) newContentPartFinding(t *testing.T, chatID uuid.UUID, createdAt time.Time) uuid.UUID {
	t.Helper()
	ctx := t.Context()
	repo := testrepo.New(tn.pool)

	partID, err := repo.InsertChatContentPartFixture(ctx, testrepo.InsertChatContentPartFixtureParams{
		ChatID:          chatID,
		ProjectID:       uuid.NullUUID{UUID: tn.projectID, Valid: true},
		Kind:            "prompt_attachment",
		ContentAssetUrl: "asset://test",
	})
	require.NoError(t, err)

	id := uuid.Must(uuid.NewV7())
	require.NoError(t, repo.InsertContentPartRiskResultFixture(ctx, testrepo.InsertContentPartRiskResultFixtureParams{
		ID:                id,
		ProjectID:         tn.projectID,
		OrganizationID:    tn.orgID,
		RiskPolicyID:      tn.policyID,
		RiskPolicyVersion: 1,
		ChatContentPartID: uuid.NullUUID{UUID: partID, Valid: true},
		Source:            "presidio",
		RuleID:            conv.ToPGText("pii.email_address"),
		Description:       conv.ToPGText("an email"),
		Match:             conv.ToPGText("alice@example.com"),
		Tags:              []string{"pii"},
	}))
	require.NoError(t, repo.UpdateRiskResultCreatedAt(ctx, testrepo.UpdateRiskResultCreatedAtParams{
		CreatedAt: pgtype.Timestamptz{Time: createdAt, Valid: true},
		ID:        id,
	}))
	return id
}

// readAll drains the source into a slice using the given criteria.
func readAll(t *testing.T, source *Source, criteria pipeline.Criteria) []SourceRow {
	t.Helper()

	out := make(chan SourceRow, 1024)
	done := make(chan error, 1)
	go func() { done <- source.Read(t.Context(), criteria, out) }()

	var rows []SourceRow
	for {
		select {
		case err := <-done:
			require.NoError(t, err)
			// Drain anything buffered before the source returned.
			for {
				select {
				case r := <-out:
					rows = append(rows, r)
				default:
					return rows
				}
			}
		case r := <-out:
			rows = append(rows, r)
		}
	}
}
