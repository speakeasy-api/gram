package riskfindings

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
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

	pool, err := infra.CloneTestDatabase(t, "riskfindings")
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

// newChat seeds a chat with chat-level user attribution — the fallback the
// source resolves when neither the anchor message nor a part's parent message
// carries user ids.
func (tn *tenant) newChat(t *testing.T, userID, externalUserID string) uuid.UUID {
	t.Helper()
	chatID, err := chatrepo.New(tn.pool).UpsertChat(t.Context(), chatrepo.UpsertChatParams{
		ID:             uuid.Must(uuid.NewV7()),
		ProjectID:      tn.projectID,
		OrganizationID: tn.orgID,
		UserID:         conv.ToPGText(userID),
		ExternalUserID: conv.ToPGText(externalUserID),
	})
	require.NoError(t, err)
	return chatID
}

// newProject seeds a second project in the tenant's org, for fixtures that
// need a project mismatch.
func (tn *tenant) newProject(t *testing.T, slug string) uuid.UUID {
	t.Helper()
	project, err := projectsrepo.New(tn.pool).CreateProject(t.Context(), projectsrepo.CreateProjectParams{
		Name:           slug,
		Slug:           slug,
		OrganizationID: tn.orgID,
	})
	require.NoError(t, err)
	return project.ID
}

// newMessage seeds a chat message stamped with createdAt — the event time the
// migration stamps into message_created_at.
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

// findingSpec describes one true-positive risk_results fixture row.
type findingSpec struct {
	source   string
	ruleID   string
	match    string
	startPos int32
	endPos   int32
	spans    []risk_analysis.FindingSpan
}

// newFinding seeds a true-positive risk_results row anchored to messageID with
// the given created_at (the scan time) and returns its id.
func (tn *tenant) newFinding(t *testing.T, messageID uuid.UUID, createdAt time.Time, spec findingSpec) uuid.UUID {
	t.Helper()
	ctx := t.Context()

	var spansJSON []byte
	if len(spec.spans) > 0 {
		b, err := json.Marshal(spec.spans)
		require.NoError(t, err)
		spansJSON = b
	}

	id := uuid.Must(uuid.NewV7())
	n, err := riskrepo.New(tn.pool).InsertRiskResults(ctx, []riskrepo.InsertRiskResultsParams{{
		ID:                id,
		ProjectID:         tn.projectID,
		OrganizationID:    tn.orgID,
		RiskPolicyID:      tn.policyID,
		RiskPolicyVersion: 1,
		ChatMessageID:     uuid.NullUUID{UUID: messageID, Valid: true},
		Source:            spec.source,
		Found:             true,
		RuleID:            conv.ToPGText(spec.ruleID),
		Description:       conv.ToPGText("a " + spec.ruleID + " finding"),
		Match:             conv.ToPGText(spec.match),
		StartPos:          pgtype.Int4{Int32: spec.startPos, Valid: true},
		EndPos:            pgtype.Int4{Int32: spec.endPos, Valid: true},
		Confidence:        pgtype.Float8{Float64: 0.9, Valid: true},
		Tags:              []string{},
		Spans:             spansJSON,
	}})
	require.NoError(t, err)
	require.EqualValues(t, 1, n)

	require.NoError(t, testrepo.New(tn.pool).UpdateRiskResultCreatedAt(ctx, testrepo.UpdateRiskResultCreatedAtParams{
		CreatedAt: pgtype.Timestamptz{Time: createdAt, Valid: true},
		ID:        id,
	}))
	return id
}

// newContentPartFinding seeds a risk_results row anchored to a chat content
// part instead of a message (chat_message_id IS NULL). Both the part and the
// finding are stamped with projectID, so passing the chat's own project makes
// the part attributable while passing a different project trips the source's
// cross-project guard (the part's chat is not in the part's project) and
// leaves attribution empty. Returns the finding id and the content part id.
func (tn *tenant) newContentPartFinding(t *testing.T, chatID, projectID uuid.UUID, createdAt time.Time) (findingID, partID uuid.UUID) {
	t.Helper()
	ctx := t.Context()
	repo := testrepo.New(tn.pool)

	partID, err := repo.InsertChatContentPartFixture(ctx, testrepo.InsertChatContentPartFixtureParams{
		ChatID:          chatID,
		ProjectID:       uuid.NullUUID{UUID: projectID, Valid: true},
		Kind:            "prompt_attachment",
		ContentAssetUrl: "asset://test",
	})
	require.NoError(t, err)

	findingID = uuid.Must(uuid.NewV7())
	require.NoError(t, repo.InsertContentPartRiskResultFixture(ctx, testrepo.InsertContentPartRiskResultFixtureParams{
		ID:                findingID,
		ProjectID:         projectID,
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
		ID:        findingID,
	}))
	return findingID, partID
}
