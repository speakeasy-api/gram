package skills_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	backgroundactivities "github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/billing"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/skills"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/skills/suggest"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

type suggestionInsightsStub struct {
	mu     sync.Mutex
	params []telemetryrepo.QuerySkillInsightsParams
	rows   []telemetryrepo.SkillInsightBucket
}

func (s *suggestionInsightsStub) QuerySkillInsights(_ context.Context, params telemetryrepo.QuerySkillInsightsParams) ([]telemetryrepo.SkillInsightBucket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.params = append(s.params, params)
	return s.rows, nil
}

type suggestionCompletionStub struct {
	mu        sync.Mutex
	responses []string
	errors    []error
	requests  []openrouter.ObjectCompletionRequest
	before    func(int)
}

func (s *suggestionCompletionStub) GetObjectCompletion(_ context.Context, request openrouter.ObjectCompletionRequest) (*openrouter.CompletionResponse, error) {
	s.mu.Lock()
	call := len(s.requests)
	s.requests = append(s.requests, request)
	var response string
	if call < len(s.responses) {
		response = s.responses[call]
	}
	var responseErr error
	if call < len(s.errors) {
		responseErr = s.errors[call]
	}
	before := s.before
	s.mu.Unlock()
	if before != nil {
		before(call)
	}
	if responseErr != nil {
		return nil, responseErr
	}
	return suggestionModelResponse(response), nil
}

func (s *suggestionCompletionStub) Requests() []openrouter.ObjectCompletionRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]openrouter.ObjectCompletionRequest(nil), s.requests...)
}

func suggestionModelResponse(text string) *openrouter.CompletionResponse {
	content := or.CreateChatAssistantMessageContentStr(text)
	message := or.CreateChatMessagesAssistant(or.ChatAssistantMessage{
		Role:             or.ChatAssistantMessageRoleAssistant,
		Content:          optionalnullable.From(&content),
		Name:             nil,
		ToolCalls:        nil,
		Refusal:          nil,
		Reasoning:        nil,
		ReasoningDetails: nil,
		Images:           nil,
		Audio:            nil,
	})
	return &openrouter.CompletionResponse{
		StartTime:    time.Time{},
		Message:      &message,
		MessageID:    "suggestion-test",
		Model:        suggest.Model,
		Usage:        openrouter.Usage{PromptTokens: 0, CompletionTokens: 0, TotalTokens: 0, Cost: nil},
		FinishReason: nil,
		ToolCalls:    nil,
		Content:      text,
	}
}

func newSuggestionEngine(t *testing.T, ti *testInstance, config suggest.Config, insights *suggestionInsightsStub, completion *suggestionCompletionStub) *suggest.Engine {
	t.Helper()
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	engine, err := suggest.NewEngine(
		config,
		testenv.NewLogger(t),
		ti.conn,
		insights,
		chatrepo.New(ti.conn),
		completion,
		openrouter.NewJudgeRateLimiter(ratelimit.NewRedisStore(redisClient)),
	)
	require.NoError(t, err)
	return engine
}

func activateSuggestionSkill(t *testing.T, ctx context.Context, ti *testInstance, name string, seenAt time.Time) {
	t.Helper()
	insertSkillObservation(t, ti, name, "", "project", "", seenAt)
	result, err := skills.ReconcileSkillObservations(ctx, ti.conn, ti.projectID, 10)
	require.NoError(t, err)
	require.Equal(t, 1, result.Processed)
}

func createSuggestionFeedback(t *testing.T, ti *testInstance, name string, count int) {
	t.Helper()
	for i := range count {
		_, err := ti.repo.CreateSkillFeedback(t.Context(), repo.CreateSkillFeedbackParams{
			ProjectID:      ti.projectID,
			SkillID:        uuid.NullUUID{},
			SkillVersionID: uuid.NullUUID{},
			SkillName:      name,
			Source:         string(skills.FeedbackSourceDev),
			Outcome:        string(skills.FeedbackOutcomeDidNotHelp),
			Note:           pgtype.Text{String: fmt.Sprintf("evidence-%d", i), Valid: true},
			SessionID:      pgtype.Text{},
			UserID:         pgtype.Text{},
			UserEmail:      pgtype.Text{},
		})
		require.NoError(t, err)
	}
}

func TestSuggestionEngineProposesAndUpdatesOneOpenRow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "engine-propose", "Base.")
	now := time.Now().UTC()
	activateSuggestionSkill(t, ctx, ti, created.Skill.Name, now)
	createSuggestionFeedback(t, ti, created.Skill.Name, 51)
	firstProposal := skillManifest(created.Skill.Name, "Improved.", "raw first proposal  ")
	secondProposal := skillManifest(created.Skill.Name, "Improved again.", "raw second proposal  ")
	completion := &suggestionCompletionStub{responses: []string{
		proposeManifest(created.Version.Content, firstProposal, "Agents keep missing the escalation step.", 50),
		proposeManifest(created.Version.Content, secondProposal, "Agents keep missing the escalation step.", 5),
	}}
	insights := &suggestionInsightsStub{}
	config := suggest.DefaultConfig()
	config.ActivityWindow = 30 * 24 * time.Hour
	engine := newSuggestionEngine(t, ti, config, insights, completion)

	first, err := engine.Run(ctx, suggest.RunInput{
		ProjectID: ti.projectID,
		SkillID:   uuid.MustParse(created.Skill.ID),
		Now:       now,
	})
	require.NoError(t, err)
	require.Equal(t, suggest.ResultProposed, first.Kind)
	require.True(t, first.SuggestionID.Valid)
	require.True(t, first.Reenqueue)
	firstSuggestion, err := ti.repo.GetLatestSkillEditSuggestion(ctx, repo.GetLatestSkillEditSuggestionParams{ProjectID: ti.projectID, SkillID: uuid.MustParse(created.Skill.ID)})
	require.NoError(t, err)
	require.Equal(t, first.SuggestionID.UUID, firstSuggestion.ID)
	require.Equal(t, firstProposal, suggestionContent(t, ctx, ti, firstSuggestion.ID, created.Version.Content))
	require.Equal(t, int64(50), suggestionFeedbackCount(t, ctx, ti, firstSuggestion.ID))
	require.Equal(t, int64(50), first.FeedbackConsumed)
	remaining, err := ti.repo.CountUnreviewedSkillFeedback(ctx, repo.CountUnreviewedSkillFeedbackParams{ProjectID: ti.projectID, SkillName: created.Skill.Name})
	require.NoError(t, err)
	require.Equal(t, int64(1), remaining)

	createSuggestionFeedback(t, ti, created.Skill.Name, 4)
	second, err := engine.Run(ctx, suggest.RunInput{
		ProjectID: ti.projectID,
		SkillID:   uuid.MustParse(created.Skill.ID),
		Now:       now.Add(8 * 24 * time.Hour),
	})
	require.NoError(t, err)
	require.Equal(t, suggest.ResultProposed, second.Kind)
	require.True(t, second.SuggestionID.Valid)
	secondSuggestion, err := ti.repo.GetLatestSkillEditSuggestion(ctx, repo.GetLatestSkillEditSuggestionParams{ProjectID: ti.projectID, SkillID: uuid.MustParse(created.Skill.ID)})
	require.NoError(t, err)
	require.Equal(t, firstSuggestion.ID, secondSuggestion.ID)
	require.Equal(t, secondProposal, suggestionContent(t, ctx, ti, secondSuggestion.ID, created.Version.Content))
	require.Equal(t, int64(5), suggestionFeedbackCount(t, ctx, ti, secondSuggestion.ID))
	require.Equal(t, int64(5), second.FeedbackConsumed)

	requests := completion.Requests()
	require.Len(t, requests, 2)
	require.Equal(t, suggest.Model, requests[0].Model)
	require.Equal(t, openrouter.KeyTypeInternal, requests[0].KeyType)
	require.Equal(t, billing.ModelUsageSourceSkillSuggestions, requests[0].UsageSource)
	require.Equal(t, billing.ModelUsageSourceSkillSuggestions, requests[0].KeySlot)
	require.Zero(t, *requests[0].Temperature)
	require.NotNil(t, requests[0].JSONSchema)
	require.Equal(t, ti.authContext.ActiveOrganizationID, requests[0].OrgID)
	require.Equal(t, ti.authContext.ActiveOrganizationID, insights.params[0].OrganizationID)
	require.Equal(t, int64((90 * 24 * time.Hour).Seconds()), insights.params[0].IntervalSeconds)
}

func TestSuggestionEngineDeclineConsumesAllLoadedFeedback(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "engine-decline", "Base.")
	now := time.Now().UTC()
	activateSuggestionSkill(t, ctx, ti, created.Skill.Name, now)
	createSuggestionFeedback(t, ti, created.Skill.Name, 5)
	createSkillFeedback(t, ti, ti.projectID, created.Skill.Name, "positive evidence")
	completion := &suggestionCompletionStub{responses: []string{
		`{"decision":"decline","changes":[],"rationale":"Agents keep missing the escalation step."}`,
		`{"decision":"decline","changes":[],"rationale":"Agents keep missing the escalation step."}`,
	}}
	engine := newSuggestionEngine(t, ti, suggest.DefaultConfig(), &suggestionInsightsStub{}, completion)

	result, err := engine.Run(ctx, suggest.RunInput{ProjectID: ti.projectID, SkillID: uuid.MustParse(created.Skill.ID), Now: now})
	require.NoError(t, err)
	require.Equal(t, suggest.ResultDeclined, result.Kind)
	require.Equal(t, int64(6), result.FeedbackConsumed)
	require.Contains(t, completion.Requests()[0].Prompt, `"outcome":"helped"`)
	_, err = ti.repo.GetOpenSkillEditSuggestion(ctx, repo.GetOpenSkillEditSuggestionParams{ProjectID: ti.projectID, SkillID: uuid.MustParse(created.Skill.ID)})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	remaining, err := ti.repo.CountUnreviewedSkillFeedback(ctx, repo.CountUnreviewedSkillFeedbackParams{ProjectID: ti.projectID, SkillName: created.Skill.Name})
	require.NoError(t, err)
	require.Zero(t, remaining)
	watermark, err := ti.repo.GetLatestSkillEditSuggestion(ctx, repo.GetLatestSkillEditSuggestionParams{ProjectID: ti.projectID, SkillID: uuid.MustParse(created.Skill.ID)})
	require.NoError(t, err)
	require.Equal(t, string(skills.EditSuggestionStatusSuperseded), watermark.Status)
	require.Empty(t, suggestionChanges(t, ctx, ti, watermark.ID))
	require.Zero(t, suggestionFeedbackCount(t, ctx, ti, watermark.ID))

	second, err := engine.Run(ctx, suggest.RunInput{ProjectID: ti.projectID, SkillID: uuid.MustParse(created.Skill.ID), Now: now.Add(24 * time.Hour)})
	require.NoError(t, err)
	require.Equal(t, suggest.ResultSkipped, second.Kind)
	require.Len(t, completion.Requests(), 1)

	createSuggestionFeedback(t, ti, created.Skill.Name, 5)
	third, err := engine.Run(ctx, suggest.RunInput{ProjectID: ti.projectID, SkillID: uuid.MustParse(created.Skill.ID), Now: now.Add(48 * time.Hour)})
	require.NoError(t, err)
	require.Equal(t, suggest.ResultDeclined, third.Kind)
	require.Len(t, completion.Requests(), 2)
}

func TestSuggestionEngineForcedRunBypassesActivityAndFeedbackThresholds(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "engine-forced", "Base.")
	now := time.Now().UTC()
	createSuggestionFeedback(t, ti, created.Skill.Name, 1)
	completion := &suggestionCompletionStub{responses: []string{
		`{"decision":"decline","changes":[],"rationale":"The evidence does not support an edit yet."}`,
	}}
	engine := newSuggestionEngine(t, ti, suggest.DefaultConfig(), &suggestionInsightsStub{}, completion)
	input := suggest.RunInput{ProjectID: ti.projectID, SkillID: uuid.MustParse(created.Skill.ID), Now: now}

	skipped, err := engine.Run(ctx, input)
	require.NoError(t, err)
	require.Equal(t, suggest.ResultSkipped, skipped.Kind)
	require.Empty(t, completion.Requests())

	forced, err := engine.RunForced(ctx, input)
	require.NoError(t, err)
	require.Equal(t, suggest.ResultDeclined, forced.Kind)
	require.Equal(t, int64(1), forced.FeedbackConsumed)
	require.Len(t, completion.Requests(), 1)
}

func TestSuggestionEngineCanonicalNoOpIsTerminalDecline(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "engine-noop", "Base.")
	now := time.Now().UTC()
	activateSuggestionSkill(t, ctx, ti, created.Skill.Name, now)
	createSuggestionFeedback(t, ti, created.Skill.Name, 5)
	baseContent := skillManifest(created.Skill.Name, "Base.", "# "+created.Skill.Name)
	completion := &suggestionCompletionStub{responses: []string{proposeManifest(created.Version.Content, baseContent, "Agents keep missing the escalation step.", 1)}}
	engine := newSuggestionEngine(t, ti, suggest.DefaultConfig(), &suggestionInsightsStub{}, completion)

	result, err := engine.Run(ctx, suggest.RunInput{ProjectID: ti.projectID, SkillID: uuid.MustParse(created.Skill.ID), Now: now})
	require.NoError(t, err)
	require.Equal(t, suggest.ResultDeclined, result.Kind)
	require.Equal(t, int64(5), result.FeedbackConsumed)
	require.Len(t, completion.Requests(), 1)
}

func TestSuggestionEngineDeclinePreservesOpenSuggestion(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "engine-decline-open", "Base.")
	skillID := uuid.MustParse(created.Skill.ID)
	baseID := uuid.MustParse(created.Version.ID)
	open, err := seedSuggestion(t, ctx, ti, seedSuggestionParams{
		ProposedDiff: diffTo(t, created.Version.Content, skillManifest(created.Skill.Name, "Existing proposal.", "existing proposal")),
		Rationale:    "existing rationale", ScoredSessionCount: 3,
		BaseVersionID: baseID, ProjectID: ti.projectID, SkillID: skillID,
	})
	require.NoError(t, err)
	now := time.Now().UTC()
	activateSuggestionSkill(t, ctx, ti, created.Skill.Name, now)
	createSuggestionFeedback(t, ti, created.Skill.Name, 5)
	completion := &suggestionCompletionStub{responses: []string{`{"decision":"decline","changes":[],"rationale":"Agents keep missing the escalation step."}`}}
	engine := newSuggestionEngine(t, ti, suggest.DefaultConfig(), &suggestionInsightsStub{}, completion)

	result, err := engine.Run(ctx, suggest.RunInput{ProjectID: ti.projectID, SkillID: skillID, Now: now})
	require.NoError(t, err)
	require.Equal(t, suggest.ResultDeclined, result.Kind)
	preserved, err := ti.repo.GetOpenSkillEditSuggestion(ctx, repo.GetOpenSkillEditSuggestionParams{ProjectID: ti.projectID, SkillID: skillID})
	require.NoError(t, err)
	require.Equal(t, open.ID, preserved.ID)
	require.Equal(t, suggestionChanges(t, ctx, ti, open.ID), suggestionChanges(t, ctx, ti, preserved.ID))
	require.Equal(t, open.Rationale, preserved.Rationale)
	require.Equal(t, string(skills.EditSuggestionStatusOpen), preserved.Status)
	require.Zero(t, suggestionFeedbackCount(t, ctx, ti, preserved.ID))
}

func TestSuggestionEngineCountsNewScoredSessionsOutsideRollingTrend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "engine-monotonic-scores", "Base.")
	skillID := uuid.MustParse(created.Skill.ID)
	baseID := uuid.MustParse(created.Version.ID)
	_, err := seedSuggestion(t, ctx, ti, seedSuggestionParams{
		ProposedDiff: diffTo(t, created.Version.Content, skillManifest(created.Skill.Name, "Existing.", "existing")), Rationale: "existing",
		ScoredSessionCount: 20, BaseVersionID: baseID, ProjectID: ti.projectID, SkillID: skillID,
	})
	require.NoError(t, err)
	latest, err := ti.repo.GetLatestSkillEditSuggestion(ctx, repo.GetLatestSkillEditSuggestionParams{ProjectID: ti.projectID, SkillID: skillID})
	require.NoError(t, err)

	seedScore := func(session string, scoredAt time.Time) {
		t.Helper()
		_, err := ti.repo.CreateScoredSkillEfficacyEvaluationFixture(ctx, repo.CreateScoredSkillEfficacyEvaluationFixtureParams{
			Surface: "assistant", SessionID: session, ChatID: uuid.New(), ScoredAt: conv.ToPGTimestamptz(scoredAt),
			BaseVersionID: baseID, ProjectID: ti.projectID, SkillID: skillID,
		})
		require.NoError(t, err)
	}
	seedScore("expired-before-watermark", latest.UpdatedAt.Time.Add(-time.Second))
	for i := range 4 {
		seedScore(fmt.Sprintf("new-session-%d", i), latest.UpdatedAt.Time.Add(time.Duration(i+1)*time.Second))
	}

	config := suggest.DefaultConfig()
	config.ActivityWindow = 30 * 24 * time.Hour
	now := latest.UpdatedAt.Time.Add(config.SuggestionFloor)
	activateSuggestionSkill(t, ctx, ti, created.Skill.Name, now)
	completion := &suggestionCompletionStub{responses: []string{`{"decision":"decline","changes":[],"rationale":"Agents keep missing the escalation step."}`}}
	insights := &suggestionInsightsStub{rows: []telemetryrepo.SkillInsightBucket{{SkillVersionID: baseID.String(), ScoredSessions: 20, ScoreSum: 16}}}
	engine := newSuggestionEngine(t, ti, config, insights, completion)

	result, err := engine.Run(ctx, suggest.RunInput{ProjectID: ti.projectID, SkillID: skillID, Now: now})
	require.NoError(t, err)
	require.Equal(t, suggest.ResultSkipped, result.Kind)
	require.Empty(t, completion.Requests())

	seedScore("new-session-4", latest.UpdatedAt.Time.Add(5*time.Second))
	result, err = engine.Run(ctx, suggest.RunInput{ProjectID: ti.projectID, SkillID: skillID, Now: now})
	require.NoError(t, err)
	require.Equal(t, suggest.ResultDeclined, result.Kind)
	require.Len(t, completion.Requests(), 1)
}

func TestSuggestionEngineInvalidProposalRetriesOnceThenConsumesFeedback(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "engine-invalid", "Base.")
	now := time.Now().UTC()
	activateSuggestionSkill(t, ctx, ti, created.Skill.Name, now)
	createSuggestionFeedback(t, ti, created.Skill.Name, 5)
	invalid := skillManifest("wrong-name", "Wrong.", "wrong")
	completion := &suggestionCompletionStub{responses: []string{
		proposeManifest(created.Version.Content, invalid, "Agents keep missing the escalation step.", 1),
		proposeManifest(created.Version.Content, invalid, "Agents keep missing the escalation step.", 1),
	}}
	engine := newSuggestionEngine(t, ti, suggest.DefaultConfig(), &suggestionInsightsStub{}, completion)

	result, err := engine.Run(ctx, suggest.RunInput{ProjectID: ti.projectID, SkillID: uuid.MustParse(created.Skill.ID), Now: now})
	require.NoError(t, err)
	require.Equal(t, suggest.ResultDeclined, result.Kind)
	require.Equal(t, int64(5), result.FeedbackConsumed)
	requests := completion.Requests()
	require.Len(t, requests, 2)
	require.Contains(t, requests[1].Prompt, "previous_validation_error")
}

func TestSuggestionEngineInvalidProposalRetryCanPropose(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "engine-invalid-retry", "Base.")
	now := time.Now().UTC()
	activateSuggestionSkill(t, ctx, ti, created.Skill.Name, now)
	createSuggestionFeedback(t, ti, created.Skill.Name, 5)
	invalid := skillManifest("wrong-name", "Wrong.", "wrong")
	valid := skillManifest(created.Skill.Name, "Corrected.", "corrected proposal")
	completion := &suggestionCompletionStub{responses: []string{
		proposeManifest(created.Version.Content, invalid, "Agents keep missing the escalation step.", 1),
		proposeManifest(created.Version.Content, valid, "Agents keep missing the escalation step.", 1),
	}}
	engine := newSuggestionEngine(t, ti, suggest.DefaultConfig(), &suggestionInsightsStub{}, completion)

	result, err := engine.Run(ctx, suggest.RunInput{ProjectID: ti.projectID, SkillID: uuid.MustParse(created.Skill.ID), Now: now})
	require.NoError(t, err)
	require.Equal(t, suggest.ResultProposed, result.Kind)
	require.True(t, result.SuggestionID.Valid)
	stored, err := ti.repo.GetLatestSkillEditSuggestion(ctx, repo.GetLatestSkillEditSuggestionParams{ProjectID: ti.projectID, SkillID: uuid.MustParse(created.Skill.ID)})
	require.NoError(t, err)
	require.Equal(t, valid, suggestionContent(t, ctx, ti, stored.ID, created.Version.Content))
	require.Len(t, completion.Requests(), 2)
}

func TestSuggestionEngineTransportFailureConsumesNothing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "engine-transport", "Base.")
	now := time.Now().UTC()
	activateSuggestionSkill(t, ctx, ti, created.Skill.Name, now)
	createSuggestionFeedback(t, ti, created.Skill.Name, 5)
	completion := &suggestionCompletionStub{errors: []error{errors.New("connection reset")}}
	engine := newSuggestionEngine(t, ti, suggest.DefaultConfig(), &suggestionInsightsStub{}, completion)

	_, err := engine.Run(ctx, suggest.RunInput{ProjectID: ti.projectID, SkillID: uuid.MustParse(created.Skill.ID), Now: now})
	require.ErrorIs(t, err, suggest.ErrRetryable)
	remaining, countErr := ti.repo.CountUnreviewedSkillFeedback(ctx, repo.CountUnreviewedSkillFeedbackParams{ProjectID: ti.projectID, SkillName: created.Skill.Name})
	require.NoError(t, countErr)
	require.Equal(t, int64(5), remaining)
}

func TestSuggestionEngineRateLimitFailureConsumesNothing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "engine-rate-limit", "Base.")
	now := time.Now().UTC()
	activateSuggestionSkill(t, ctx, ti, created.Skill.Name, now)
	createSuggestionFeedback(t, ti, created.Skill.Name, 5)
	completion := &suggestionCompletionStub{responses: []string{`{"decision":"decline","proposed_skill_md":"","rationale":"unused"}`}}
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	limiter := ratelimit.New(ratelimit.NewRedisStore(redisClient), t.Name(), ratelimit.Rate{Tokens: 1, Interval: time.Hour, Burst: 1})
	key := openrouter.JudgeRateLimitKey(openrouter.PlatformKey(), suggest.Model)
	allowed, err := limiter.Allow(ctx, key)
	require.NoError(t, err)
	require.True(t, allowed.Allowed)
	engine, err := suggest.NewEngine(suggest.DefaultConfig(), testenv.NewLogger(t), ti.conn, &suggestionInsightsStub{}, chatrepo.New(ti.conn), completion, limiter)
	require.NoError(t, err)

	_, err = engine.Run(ctx, suggest.RunInput{ProjectID: ti.projectID, SkillID: uuid.MustParse(created.Skill.ID), Now: now})
	require.ErrorIs(t, err, suggest.ErrRetryable)
	require.Empty(t, completion.Requests())
	remaining, countErr := ti.repo.CountUnreviewedSkillFeedback(ctx, repo.CountUnreviewedSkillFeedbackParams{ProjectID: ti.projectID, SkillName: created.Skill.Name})
	require.NoError(t, countErr)
	require.Equal(t, int64(5), remaining)
}

func TestSuggestionEngineRejectedRequestIsModelFailure(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "engine-model-failure", "Base.")
	now := time.Now().UTC()
	activateSuggestionSkill(t, ctx, ti, created.Skill.Name, now)
	createSuggestionFeedback(t, ti, created.Skill.Name, 5)
	completion := &suggestionCompletionStub{errors: []error{fmt.Errorf("rejected: %w", openrouter.ErrBadRequest)}}
	engine := newSuggestionEngine(t, ti, suggest.DefaultConfig(), &suggestionInsightsStub{}, completion)

	_, err := engine.Run(ctx, suggest.RunInput{ProjectID: ti.projectID, SkillID: uuid.MustParse(created.Skill.ID), Now: now})
	require.ErrorIs(t, err, suggest.ErrModelFailure)
	require.NotErrorIs(t, err, suggest.ErrRetryable)
}

func TestSuggestionActivityMakesModelFailureNonRetryable(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "activity-model-failure", "Base.")
	now := time.Now().UTC()
	activateSuggestionSkill(t, ctx, ti, created.Skill.Name, now)
	createSuggestionFeedback(t, ti, created.Skill.Name, 5)
	completion := &suggestionCompletionStub{errors: []error{fmt.Errorf("rejected: %w", openrouter.ErrBadRequest)}}
	engine := newSuggestionEngine(t, ti, suggest.DefaultConfig(), &suggestionInsightsStub{}, completion)
	analyzer := backgroundactivities.NewSkillSuggestionAnalyzer(ti.conn, engine, nil)

	_, err := analyzer.AnalyzeSkillSuggestion(ctx, backgroundactivities.AnalyzeSkillSuggestionParams{
		SkillSuggestionIdentity: backgroundactivities.SkillSuggestionIdentity{ProjectID: ti.projectID, SkillID: uuid.MustParse(created.Skill.ID), Force: false},
		Now:                     now,
	})
	require.Error(t, err)
	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	require.True(t, appErr.NonRetryable())
}

func TestSuggestionEngineSupersedesStaleOpenBeforeInsert(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "engine-stale-open", "Base.")
	skillID := uuid.MustParse(created.Skill.ID)
	oldBaseID := uuid.MustParse(created.Version.ID)
	stale, err := seedSuggestion(t, ctx, ti, seedSuggestionParams{
		ProposedDiff:       diffTo(t, created.Version.Content, skillManifest(created.Skill.Name, "Stale.", "stale")),
		Rationale:          "stale",
		ScoredSessionCount: 1,
		BaseVersionID:      oldBaseID,
		ProjectID:          ti.projectID,
		SkillID:            skillID,
	})
	require.NoError(t, err)
	newBase, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID:               created.Skill.ID,
		Content:          skillManifest(created.Skill.Name, "Current.", "current base"),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	now := time.Now().UTC()
	activateSuggestionSkill(t, ctx, ti, created.Skill.Name, now)
	createSuggestionFeedback(t, ti, created.Skill.Name, 5)
	proposal := skillManifest(created.Skill.Name, "Proposed.", "new proposal")
	completion := &suggestionCompletionStub{responses: []string{proposeManifest(newBase.Version.Content, proposal, "Agents keep missing the escalation step.", 1)}}
	insights := &suggestionInsightsStub{}
	engine := newSuggestionEngine(t, ti, suggest.DefaultConfig(), insights, completion)

	result, err := engine.Run(ctx, suggest.RunInput{ProjectID: ti.projectID, SkillID: skillID, Now: now})
	require.NoError(t, err)
	require.Equal(t, suggest.ResultProposed, result.Kind)
	require.True(t, result.SuggestionID.Valid)
	require.NotEqual(t, stale.ID, result.SuggestionID.UUID)
	newSuggestion, err := ti.repo.GetLatestSkillEditSuggestion(ctx, repo.GetLatestSkillEditSuggestionParams{ProjectID: ti.projectID, SkillID: skillID})
	require.NoError(t, err)
	require.Equal(t, uuid.MustParse(newBase.Version.ID), newSuggestion.BaseVersionID)
	require.ElementsMatch(t, []string{newBase.Version.ID, created.Version.ID}, insights.params[0].SkillVersionIDs)
	latestStale, err := ti.repo.GetLatestSkillEditSuggestion(ctx, repo.GetLatestSkillEditSuggestionParams{ProjectID: ti.projectID, SkillID: skillID})
	require.NoError(t, err)
	require.Equal(t, result.SuggestionID.UUID, latestStale.ID)
	_, err = ti.repo.UpdateOpenSkillEditSuggestion(ctx, repo.UpdateOpenSkillEditSuggestionParams{
		Rationale:          "cannot-update-stale",
		ScoredSessionCount: 0,
		ProjectID:          ti.projectID,
		SkillID:            skillID,
		BaseVersionID:      oldBaseID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestSuggestionEngineBaseRaceReenqueuesAndConsumesNothing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "engine-base-race", "Base.")
	now := time.Now().UTC()
	activateSuggestionSkill(t, ctx, ti, created.Skill.Name, now)
	createSuggestionFeedback(t, ti, created.Skill.Name, 5)
	proposal := skillManifest(created.Skill.Name, "Proposed.", "proposal")
	completion := &suggestionCompletionStub{
		responses: []string{proposeManifest(created.Version.Content, proposal, "Agents keep missing the escalation step.", 1)},
		before: func(call int) {
			if call != 0 {
				return
			}
			_, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
				ID:               created.Skill.ID,
				Content:          skillManifest(created.Skill.Name, "Moved.", "moved base"),
				SessionToken:     nil,
				ApikeyToken:      nil,
				ProjectSlugInput: nil,
			})
			require.NoError(t, err)
		},
	}
	engine := newSuggestionEngine(t, ti, suggest.DefaultConfig(), &suggestionInsightsStub{}, completion)

	result, err := engine.Run(ctx, suggest.RunInput{ProjectID: ti.projectID, SkillID: uuid.MustParse(created.Skill.ID), Now: now})
	require.NoError(t, err)
	require.Equal(t, suggest.ResultBaseMoved, result.Kind)
	require.True(t, result.Reenqueue)
	require.Zero(t, result.FeedbackConsumed)
	remaining, err := ti.repo.CountUnreviewedSkillFeedback(ctx, repo.CountUnreviewedSkillFeedbackParams{ProjectID: ti.projectID, SkillName: created.Skill.Name})
	require.NoError(t, err)
	require.Equal(t, int64(5), remaining)
}

func TestSuggestionEngineDismissalRaceConsumesEvidenceWithoutReenqueue(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "engine-suggestion-race", "Base.")
	skillID := uuid.MustParse(created.Skill.ID)
	open, err := seedSuggestion(t, ctx, ti, seedSuggestionParams{
		ProposedDiff: diffTo(t, created.Version.Content, skillManifest(created.Skill.Name, "Existing.", "existing")), Rationale: "existing",
		ScoredSessionCount: 0, BaseVersionID: uuid.MustParse(created.Version.ID),
		ProjectID: ti.projectID, SkillID: skillID,
	})
	require.NoError(t, err)
	now := time.Now().UTC()
	activateSuggestionSkill(t, ctx, ti, created.Skill.Name, now)
	createSuggestionFeedback(t, ti, created.Skill.Name, 5)
	proposal := skillManifest(created.Skill.Name, "Proposed.", "proposal")
	completion := &suggestionCompletionStub{
		responses: []string{proposeManifest(created.Version.Content, proposal, "Agents keep missing the escalation step.", 1)},
		before: func(call int) {
			if call != 0 {
				return
			}
			_, dismissErr := ti.repo.DismissSkillEditSuggestion(ctx, repo.DismissSkillEditSuggestionParams{
				ProjectID: ti.projectID, SkillID: skillID, ID: open.ID,
			})
			require.NoError(t, dismissErr)
		},
	}
	engine := newSuggestionEngine(t, ti, suggest.DefaultConfig(), &suggestionInsightsStub{}, completion)

	result, err := engine.Run(ctx, suggest.RunInput{ProjectID: ti.projectID, SkillID: skillID, Now: now})
	require.NoError(t, err)
	require.Equal(t, suggest.ResultDeclined, result.Kind)
	require.False(t, result.Reenqueue)
	require.Equal(t, int64(5), result.FeedbackConsumed)
	remaining, err := ti.repo.CountUnreviewedSkillFeedback(ctx, repo.CountUnreviewedSkillFeedbackParams{ProjectID: ti.projectID, SkillName: created.Skill.Name})
	require.NoError(t, err)
	require.Zero(t, remaining)
	dismissed, err := ti.repo.GetLatestSkillEditSuggestion(ctx, repo.GetLatestSkillEditSuggestionParams{ProjectID: ti.projectID, SkillID: skillID})
	require.NoError(t, err)
	require.Equal(t, open.ID, dismissed.ID)
	require.Equal(t, string(skills.EditSuggestionStatusDismissed), dismissed.Status)
	require.Zero(t, suggestionFeedbackCount(t, ctx, ti, dismissed.ID))
	_, err = ti.repo.GetOpenSkillEditSuggestion(ctx, repo.GetOpenSkillEditSuggestionParams{ProjectID: ti.projectID, SkillID: skillID})
	require.ErrorIs(t, err, pgx.ErrNoRows)

	second, err := engine.Run(ctx, suggest.RunInput{ProjectID: ti.projectID, SkillID: skillID, Now: now})
	require.NoError(t, err)
	require.Equal(t, suggest.ResultSkipped, second.Kind)
	require.Len(t, completion.Requests(), 1)
}

func TestSuggestionEngineOtherSuggestionRaceReenqueuesAndConsumesNothing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "engine-other-suggestion-race", "Base.")
	skillID := uuid.MustParse(created.Skill.ID)
	baseID := uuid.MustParse(created.Version.ID)
	_, err := seedSuggestion(t, ctx, ti, seedSuggestionParams{
		ProposedDiff: diffTo(t, created.Version.Content, skillManifest(created.Skill.Name, "Existing.", "existing")), Rationale: "existing",
		ScoredSessionCount: 0, BaseVersionID: baseID, ProjectID: ti.projectID, SkillID: skillID,
	})
	require.NoError(t, err)
	now := time.Now().UTC()
	activateSuggestionSkill(t, ctx, ti, created.Skill.Name, now)
	createSuggestionFeedback(t, ti, created.Skill.Name, 5)
	proposal := skillManifest(created.Skill.Name, "Proposed.", "proposal")
	completion := &suggestionCompletionStub{
		responses: []string{proposeManifest(created.Version.Content, proposal, "Agents keep missing the escalation step.", 1)},
		before: func(call int) {
			if call != 0 {
				return
			}
			_, updateErr := ti.repo.UpdateOpenSkillEditSuggestion(ctx, repo.UpdateOpenSkillEditSuggestionParams{
				Rationale:          "changed",
				ScoredSessionCount: 0, ProjectID: ti.projectID, SkillID: skillID, BaseVersionID: baseID,
			})
			require.NoError(t, updateErr)
		},
	}
	engine := newSuggestionEngine(t, ti, suggest.DefaultConfig(), &suggestionInsightsStub{}, completion)

	result, err := engine.Run(ctx, suggest.RunInput{ProjectID: ti.projectID, SkillID: skillID, Now: now})
	require.NoError(t, err)
	require.Equal(t, suggest.ResultBaseMoved, result.Kind)
	require.True(t, result.Reenqueue)
	require.Zero(t, result.FeedbackConsumed)
	remaining, err := ti.repo.CountUnreviewedSkillFeedback(ctx, repo.CountUnreviewedSkillFeedbackParams{ProjectID: ti.projectID, SkillName: created.Skill.Name})
	require.NoError(t, err)
	require.Equal(t, int64(5), remaining)
}

func TestSuggestionEngineMissingSkillIsSkipped(t *testing.T) {
	t.Parallel()

	_, ti := newTestService(t)
	completion := &suggestionCompletionStub{}
	engine := newSuggestionEngine(t, ti, suggest.DefaultConfig(), &suggestionInsightsStub{}, completion)

	result, err := engine.Run(t.Context(), suggest.RunInput{ProjectID: ti.projectID, SkillID: uuid.New(), Now: time.Now().UTC()})
	require.NoError(t, err)
	require.Equal(t, suggest.ResultSkipped, result.Kind)
	require.Empty(t, completion.Requests())
}

func TestSuggestionEngineMissingValidBaseIsSkipped(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created, err := ti.service.Create(ctx, &gen.CreatePayload{
		Content: "---\nname: engine-no-valid-base\n---\n\ninvalid\n", SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.False(t, created.Version.SpecValid)
	now := time.Now().UTC()
	activateSuggestionSkill(t, ctx, ti, created.Skill.Name, now)
	completion := &suggestionCompletionStub{}
	engine := newSuggestionEngine(t, ti, suggest.DefaultConfig(), &suggestionInsightsStub{}, completion)

	result, err := engine.Run(ctx, suggest.RunInput{ProjectID: ti.projectID, SkillID: uuid.MustParse(created.Skill.ID), Now: now})
	require.NoError(t, err)
	require.Equal(t, suggest.ResultSkipped, result.Kind)
	require.Empty(t, completion.Requests())
}

func replayableSkill(name, description, firstLine, lastLine string) string {
	return fmt.Sprintf(
		"---\nname: %s\ndescription: %s\n---\n\n%s\nLine two.\nLine three.\nLine four.\nLine five.\nLine six.\nLine seven.\nLine eight.\nLine nine.\n%s\n",
		name, description, firstLine, lastLine,
	)
}

func TestSuggestionEngineReplaysOpenSuggestionOntoNewerVersion(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	name := "engine-replay"
	created, err := ti.service.Create(ctx, &gen.CreatePayload{
		Content:          replayableSkill(name, "Base.", "Line one.", "Line ten."),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	skillID := uuid.MustParse(created.Skill.ID)

	open, err := seedSuggestion(t, ctx, ti, seedSuggestionParams{
		ProposedDiff:       diffTo(t, created.Version.Content, replayableSkill(name, "Base.", "Line one.", "Line ten, with detail.")),
		Rationale:          "The last step needs detail.",
		ScoredSessionCount: 0,
		BaseVersionID:      uuid.MustParse(created.Version.ID),
		ProjectID:          ti.projectID,
		SkillID:            skillID,
	})
	require.NoError(t, err)

	// A later version edits a line the suggestion does not touch.
	moved, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID:               created.Skill.ID,
		Content:          replayableSkill(name, "Base.", "Line one, revised.", "Line ten."),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	now := time.Now().UTC()
	activateSuggestionSkill(t, ctx, ti, name, now)
	completion := &suggestionCompletionStub{}
	engine := newSuggestionEngine(t, ti, suggest.DefaultConfig(), &suggestionInsightsStub{}, completion)

	result, err := engine.Run(ctx, suggest.RunInput{ProjectID: ti.projectID, SkillID: skillID, Now: now})
	require.NoError(t, err)
	require.Equal(t, suggest.ResultSkipped, result.Kind)
	require.Empty(t, completion.Requests())

	replayed, err := ti.repo.GetOpenSkillEditSuggestion(ctx, repo.GetOpenSkillEditSuggestionParams{ProjectID: ti.projectID, SkillID: skillID})
	require.NoError(t, err)
	require.Equal(t, open.ID, replayed.ID)
	require.Equal(t, uuid.MustParse(moved.Version.ID), replayed.BaseVersionID)
	require.Equal(t,
		replayableSkill(name, "Base.", "Line one, revised.", "Line ten, with detail."),
		suggestionContent(t, ctx, ti, replayed.ID, moved.Version.Content),
	)
}

func TestSuggestionEngineRetiresOpenSuggestionThatNoLongerApplies(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	name := "engine-retire"
	created, err := ti.service.Create(ctx, &gen.CreatePayload{
		Content:          replayableSkill(name, "Base.", "Line one.", "Line ten."),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	skillID := uuid.MustParse(created.Skill.ID)

	open, err := seedSuggestion(t, ctx, ti, seedSuggestionParams{
		ProposedDiff:       diffTo(t, created.Version.Content, replayableSkill(name, "Base.", "Line one.", "Line ten, with detail.")),
		Rationale:          "The last step needs detail.",
		ScoredSessionCount: 0,
		BaseVersionID:      uuid.MustParse(created.Version.ID),
		ProjectID:          ti.projectID,
		SkillID:            skillID,
	})
	require.NoError(t, err)

	// A later version rewrites the very line the suggestion edits.
	_, err = ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID:               created.Skill.ID,
		Content:          replayableSkill(name, "Base.", "Line one.", "Line ten, rewritten by hand."),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	now := time.Now().UTC()
	activateSuggestionSkill(t, ctx, ti, name, now)
	completion := &suggestionCompletionStub{}
	engine := newSuggestionEngine(t, ti, suggest.DefaultConfig(), &suggestionInsightsStub{}, completion)

	_, err = engine.Run(ctx, suggest.RunInput{ProjectID: ti.projectID, SkillID: skillID, Now: now})
	require.NoError(t, err)

	_, err = ti.repo.GetOpenSkillEditSuggestion(ctx, repo.GetOpenSkillEditSuggestionParams{ProjectID: ti.projectID, SkillID: skillID})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	retired, err := ti.repo.GetLatestSkillEditSuggestion(ctx, repo.GetLatestSkillEditSuggestionParams{ProjectID: ti.projectID, SkillID: skillID})
	require.NoError(t, err)
	require.Equal(t, open.ID, retired.ID)
	require.Equal(t, string(skills.EditSuggestionStatusSuperseded), retired.Status)
}

// proposeManifest renders a model response that replaces the whole manifest in
// one change, citing the first refCount feedback items as its evidence. Tests
// that care about how a proposal splits into changes build the response
// themselves.
func proposeManifest(base, proposed, rationale string, refCount int) string {
	refs := make([]int, refCount)
	for i := range refs {
		refs[i] = i + 1
	}
	body, err := json.Marshal(map[string]any{
		"decision": "propose",
		"changes": []map[string]any{{
			"find": base, "replace": proposed, "rationale": rationale, "evidence": refs,
		}},
		"rationale": rationale,
	})
	if err != nil {
		panic(err)
	}
	return string(body)
}

func (s *suggestionCompletionStub) ResolveKey(_ context.Context, _ string, _ string, _ billing.ModelUsageSource, _ openrouter.KeyType) (openrouter.ResolvedKey, error) {
	return openrouter.PlatformKey(), nil
}
