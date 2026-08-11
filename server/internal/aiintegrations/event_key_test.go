package aiintegrations

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	anthropicapi "github.com/speakeasy-api/gram/server/internal/thirdparty/anthropic"
	cursorapi "github.com/speakeasy-api/gram/server/internal/thirdparty/cursor"
)

// These digests freeze each provider's event-key encoding. A failure here
// means the change would orphan every event_hash already ingested into
// ClickHouse — do not update the expected value unless dedup continuity is
// intentionally being broken.

func TestCursorUsageEventKeyGolden(t *testing.T) {
	t.Parallel()

	event := cursorapi.UsageEvent{
		Timestamp:        time.Date(2026, 7, 16, 10, 30, 0, 0, time.UTC),
		Model:            "claude-4.5-opus",
		Kind:             "Included in Business",
		ChargedCents:     12.5,
		MaxMode:          false,
		IsHeadless:       false,
		IsTokenBasedCall: false,
		TokenUsage: cursorapi.TokenUsage{
			InputTokens:      100,
			OutputTokens:     200,
			CacheReadTokens:  300,
			CacheWriteTokens: 400,
			TotalCents:       12.5,
		},
		// Unnormalized on purpose: pins that normalization happens inside
		// the generator.
		UserEmail: "User@Example.com ",
	}

	require.Equal(t,
		"aeeb699366765bfb9dc8db4c957dbc2beb5150032851feb3c66d7387ca9eb9b6",
		generateCursorUsageEventHash(event))
}

func TestClaudeChatUsageRowKeyGolden(t *testing.T) {
	t.Parallel()

	row := anthropicapi.UserUsageRow{
		Actor:                anthropicapi.AnalyticsActor{UserID: "user_abc", Email: nil, Name: nil, Deleted: false},
		StartingAt:           "2026-07-16T10:30:00Z",
		EndingAt:             "2026-07-16T10:31:00Z",
		Model:                "claude-opus-4-8",
		Product:              "chat",
		UncachedInputTokens:  100,
		OutputTokens:         200,
		CacheReadInputTokens: 300,
		CacheCreation: anthropicapi.AnalyticsCacheCreation{
			Ephemeral1hInputTokens: 400,
			Ephemeral5mInputTokens: 500,
		},
		TotalTokens: 1500,
		Requests:    3,
	}

	require.Equal(t,
		"17f72799260eb2a2ae3bbf985714d5df70adfd633057b52e0fe04fbd9f28e292",
		generateClaudeChatUsageRowHash(row))
}

func TestClaudeChatCostRowKeyGolden(t *testing.T) {
	t.Parallel()

	row := anthropicapi.UserCostRow{
		Actor:      anthropicapi.AnalyticsActor{UserID: "user_abc", Email: nil, Name: nil, Deleted: false},
		StartingAt: "2026-07-16T10:30:00Z",
		EndingAt:   "2026-07-16T10:31:00Z",
		Model:      "claude-opus-4-8",
		Product:    "chat",
		Amount:     "150.5",
		ListAmount: "150.5",
		Currency:   "USD",
		Requests:   3,
	}

	require.Equal(t,
		"c22d4fd78b2f1da2e42c24b5ba7aee269fe44375c69ed0a23ff4a9783b3079f2",
		generateClaudeChatCostRowHash(row))
}

func TestCodexCostEventKeyGolden(t *testing.T) {
	t.Parallel()

	event := codexCostEvent{
		// Untrimmed on purpose: pins that normalization happens inside
		// the generator.
		EventID:   " event_abc123 ",
		Type:      codexComplianceCostsEventType,
		Timestamp: "2026-07-16T10:30:00Z",
		Payload: codexCostPayload{
			Day:            "2026-07-16",
			Hour:           10,
			OrganizationID: "org-openai",
			Identity:       codexCostIdentity{UserID: "user_abc", Email: "User@Example.com", Name: "User", Groups: nil},
			Product:        "codex",
			Client:         "github",
			Surface:        "github_code_review",
			Model:          "gpt-5.5",
			ServiceTier:    "default",
			Reasoning:      "high",
			Measures: codexCostMeasures{
				Usage: codexCostUsage{
					TextInputTokens:       100,
					TextCachedInputTokens: 200,
					TextOutputTokens:      300,
				},
				Billing: nil,
			},
		},
	}

	require.Equal(t,
		"234e1992eb486518c57a9a433ca9d3fe05c8cf55f07c463be7a81a23223f087d",
		generateCodexCostEventHash(event.EventID))
}

func TestEventKeyPanicsOnUnsupportedFieldType(t *testing.T) {
	t.Parallel()

	require.PanicsWithValue(t,
		"eventKey: no stable rendering for field 0 (int)",
		func() { eventKey{42}.hash() })
}
