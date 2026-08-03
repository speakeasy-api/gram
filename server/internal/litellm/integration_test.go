package litellm

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hooksgen "github.com/speakeasy-api/gram/server/gen/hooks"
	gen "github.com/speakeasy-api/gram/server/gen/litellm"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/message"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/risk"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

type recordingScanner struct {
	result             *risk.ScanResult
	seenUserIDs        []string
	acknowledgementHit bool
	challenges         int
}

func requireChatMessages(t *testing.T, ctx context.Context, conn *pgxpool.Pool, params chatrepo.ListChatMessagesParams, count int) []chatrepo.ChatMessage {
	t.Helper()
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		messages, err := chatrepo.New(conn).ListChatMessages(ctx, params)
		assert.NoError(collect, err)
		assert.Len(collect, messages, count)
	}, 2*time.Second, 10*time.Millisecond)
	messages, err := chatrepo.New(conn).ListChatMessages(ctx, params)
	require.NoError(t, err)
	return messages
}

func (s *recordingScanner) ScanForEnforcement(_ context.Context, _ string, _ uuid.UUID, userID string, _ string, _ message.Type, _ string) (*risk.ScanResult, error) {
	s.seenUserIDs = append(s.seenUserIDs, userID)
	return s.result, nil
}

func (s *recordingScanner) LookupShadowMCPBlockingPolicy(_ context.Context, _ string, _ uuid.UUID, _ string) (*risk.ShadowMCPPolicy, error) {
	return nil, nil
}

func (s *recordingScanner) HasEnabledShadowMCPPolicy(_ context.Context, _ uuid.UUID) (bool, error) {
	return false, nil
}

func (s *recordingScanner) HasAcknowledgedChallenge(_ context.Context, _ uuid.UUID, _, _, _, _ string) bool {
	s.acknowledgementHit = true
	return false
}

func (s *recordingScanner) RecordPolicyChallenge(_ context.Context, _ string, _ uuid.UUID, _, _, _, _, _, _, _ string) {
	s.challenges++
}

func TestRealHooksPersistsMixedCaseMemberAndDedupesRetry(t *testing.T) {
	t.Parallel()
	ctx, ti := newRealTestService(t, nil)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	userID := "user_" + uuid.NewString()
	storedEmail := "Member." + uuid.NewString() + "@Example.Test"
	_, err := usersrepo.New(ti.conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID:          userID,
		Email:       storedEmail,
		DisplayName: "Test Member",
		PhotoUrl:    pgtype.Text{},
		Admin:       false,
	})
	require.NoError(t, err)
	_, err = organizationsrepo.New(ti.conn).UpsertOrganizationUserRelationship(ctx, organizationsrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         conv.ToPGText(userID),
	})
	require.NoError(t, err)

	payload := testPayload()
	callID := "mixed-case-" + uuid.NewString()
	payload.LitellmCallID = &callID
	payload.Texts = []string{"safe prompt"}
	payload.RequestData.UserAPIKeyUserEmail = new(storedEmail)
	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, gen.LiteLLMGuardrailAction("NONE"), result.Action)
	result, err = ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, gen.LiteLLMGuardrailAction("NONE"), result.Action)

	messages := requireChatMessages(t, ctx, ti.conn, chatrepo.ListChatMessagesParams{
		ChatID:    chat.SessionIDToChatID(callID),
		ProjectID: *authCtx.ProjectID,
	}, 1)
	require.Equal(t, "safe prompt", messages[0].Content)
	require.Equal(t, "litellm", messages[0].Source.String)
	require.Equal(t, userID, messages[0].UserID.String)
	require.Equal(t, conv.NormalizeEmail(storedEmail), messages[0].ExternalUserID.String)
}

func TestRealHooksBlocksAndCapturesPolicyMessage(t *testing.T) {
	t.Parallel()
	userMessage := "This prompt is not permitted."
	scanner := &recordingScanner{
		result: &risk.ScanResult{
			Action:           "block",
			PolicyID:         uuid.NewString(),
			PolicyName:       "test block policy",
			Source:           "test",
			MessageType:      message.User,
			RuleID:           "test-rule",
			Description:      "matched test policy",
			UserMessage:      &userMessage,
			MatchedValue:     "",
			Entity:           "",
			CallFingerprint:  "fingerprint",
			DeadLetterReason: "",
		},
		seenUserIDs:        nil,
		acknowledgementHit: false,
		challenges:         0,
	}
	ctx, ti := newRealTestService(t, scanner)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	payload := testPayload()
	callID := "blocked-" + uuid.NewString()
	payload.LitellmCallID = &callID
	payload.Texts = []string{"blocked prompt"}

	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, gen.LiteLLMGuardrailAction("BLOCKED"), result.Action)
	require.Equal(t, userMessage, *result.BlockedReason)
	messages := requireChatMessages(t, ctx, ti.conn, chatrepo.ListChatMessagesParams{
		ChatID:    chat.SessionIDToChatID(callID),
		ProjectID: *authCtx.ProjectID,
	}, 1)
	require.Equal(t, "blocked prompt", messages[0].Content)
}

func TestRealHooksTreatsWarnAsBlockWithoutChallenge(t *testing.T) {
	t.Parallel()
	userMessage := "Warning policy blocks model traffic."
	scanner := &recordingScanner{
		result: &risk.ScanResult{
			Action:           "warn",
			PolicyID:         uuid.NewString(),
			PolicyName:       "test warn policy",
			Source:           "test",
			MessageType:      message.User,
			RuleID:           "warn-rule",
			Description:      "matched warning policy",
			UserMessage:      &userMessage,
			MatchedValue:     "sensitive match",
			Entity:           "secret",
			CallFingerprint:  "fingerprint",
			DeadLetterReason: "",
		},
		seenUserIDs:        nil,
		acknowledgementHit: false,
		challenges:         0,
	}
	ctx, ti := newRealTestService(t, scanner)
	payload := testPayload()
	payload.LitellmCallID = new("warn-" + uuid.NewString())
	payload.Texts = []string{"warning prompt"}

	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, gen.LiteLLMGuardrailAction("BLOCKED"), result.Action)
	require.Equal(t, userMessage, *result.BlockedReason)
	require.NotContains(t, *result.BlockedReason, "ack")
	require.False(t, scanner.acknowledgementHit)
	require.Zero(t, scanner.challenges)
}

func TestRealHooksNeverUsesEndUserKeyOwnerOrCachedActor(t *testing.T) {
	t.Parallel()
	scanner := &recordingScanner{result: nil, seenUserIDs: nil, acknowledgementHit: false, challenges: 0}
	ctx, ti := newRealTestService(t, scanner)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	sessionID := "cached-identity-" + uuid.NewString()
	_, err := ti.hooks.IngestAuthenticated(ctx, authCtx, &hooksgen.IngestPayload{
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Replayed:         nil,
		SchemaVersion:    "hook.ingest.v1",
		IdempotencyKey:   new("seed-" + uuid.NewString()),
		Source: &hooksgen.HookIngestSource{
			Adapter:        "test",
			AdapterVersion: nil,
			RawEventName:   nil,
			Hostname:       nil,
			UserEmail:      nil,
		},
		Session: &hooksgen.HookIngestSession{ID: &sessionID, TurnID: nil, Cwd: nil, Model: nil},
		Event:   &hooksgen.HookIngestEvent{Type: "session.started", OccurredAt: nil},
		Data:    nil,
		Raw:     nil,
	})
	require.NoError(t, err)

	payload := testPayload()
	callID := "unattributed-" + uuid.NewString()
	payload.LitellmCallID = &callID
	payload.Texts = []string{"unattributed prompt"}
	payload.RequestHeaders = map[string]string{"x-gram-session-id": sessionID}
	payload.RequestData.UserAPIKeyUserEmail = new("missing." + uuid.NewString() + "@example.test")
	payload.RequestData.UserAPIKeyEndUserID = new(authCtx.UserID)
	payload.RequestData.UserAPIKeyUserID = new(authCtx.UserID)
	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, gen.LiteLLMGuardrailAction("NONE"), result.Action)

	missingEmail := testPayload()
	missingCallID := "missing-email-" + uuid.NewString()
	missingEmail.LitellmCallID = &missingCallID
	missingEmail.Texts = []string{"missing email prompt"}
	missingEmail.RequestData.UserAPIKeyEndUserID = new(authCtx.UserID)
	result, err = ti.service.Ingest(ctx, missingEmail)
	require.NoError(t, err)
	require.Equal(t, gen.LiteLLMGuardrailAction("NONE"), result.Action)
	require.Equal(t, []string{"", ""}, scanner.seenUserIDs)

	messages := requireChatMessages(t, ctx, ti.conn, chatrepo.ListChatMessagesParams{
		ChatID:    chat.SessionIDToChatID(sessionID),
		ProjectID: *authCtx.ProjectID,
	}, 1)
	require.False(t, messages[0].UserID.Valid)
	require.NotEqual(t, authCtx.UserID, messages[0].ExternalUserID.String)
	missingMessages := requireChatMessages(t, ctx, ti.conn, chatrepo.ListChatMessagesParams{
		ChatID:    chat.SessionIDToChatID(missingCallID),
		ProjectID: *authCtx.ProjectID,
	}, 1)
	require.False(t, missingMessages[0].UserID.Valid)
	require.False(t, missingMessages[0].ExternalUserID.Valid)
}
