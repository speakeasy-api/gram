package aiintegrations

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aiintegrations/timewindowpoller"
	codexapi "github.com/speakeasy-api/gram/server/internal/thirdparty/codex"
)

// logFilePager is the pagination sub-interface shared by the Codex COSTS and
// ChatGPT CONVERSATION_MESSAGE sources. Their listing state machines are
// deliberate copies (see the comment on chatgptConversationSource), so the
// suite below runs the same protocol edge cases against BOTH sources — a
// pagination fix applied to one copy fails here until the other copy gets it
// too.
type logFilePager interface {
	UpperBound(ctx context.Context, endTime time.Time) (time.Time, error)
	FetchPage(ctx context.Context, start, end time.Time, pageToken string) (timewindowpoller.Page[[]codexapi.LogFile], error)
}

type logFileSourceCase struct {
	name      string
	eventType string
	lookback  time.Duration
	newConfig func() Config
	newSource func(cfg Config, client codexComplianceClient) logFilePager
}

func logFileSourceCases() []logFileSourceCase {
	return []logFileSourceCase{
		{
			name:      "codex_costs",
			eventType: codexComplianceCostsEventType,
			lookback:  codexComplianceInitialLookback,
			newConfig: codexCostConfig,
			newSource: func(cfg Config, client codexComplianceClient) logFilePager {
				return &codexCostSource{
					client:      client,
					cfg:         cfg,
					pageLimit:   codexCompliancePageLimit,
					processPage: nil,
					progress:    &CodexCostSyncProgress{},
				}
			},
		},
		{
			name:      "chatgpt_conversations",
			eventType: chatgptConversationEventType,
			lookback:  chatgptComplianceInitialLookback,
			newConfig: chatgptConversationConfig,
			newSource: func(cfg Config, client codexComplianceClient) logFilePager {
				return newChatGPTTestSource(cfg, client)
			},
		},
		{
			name:      "codex_cloud_sessions",
			eventType: codexCloudEventType,
			lookback:  chatgptComplianceInitialLookback,
			newConfig: chatgptConversationConfig,
			newSource: func(cfg Config, client codexComplianceClient) logFilePager {
				return newCodexCloudTestSource(cfg, client)
			},
		},
	}
}

func TestLogFileSourcesUpperBoundReturnsStartWhenNoLogs(t *testing.T) {
	t.Parallel()

	endTime := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	for _, tc := range logFileSourceCases() {
		cfg := tc.newConfig()
		cfg.PollWatermarkAt = time.Time{}
		cfg.PollCheckpoint = timewindowpoller.CompletedCheckpoint(time.Time{})
		start := endTime.Add(-tc.lookback)
		client := &stubCodexComplianceClient{
			listPages: []*codexapi.LogsPage{
				{Data: nil, HasMore: false, LastEndTime: time.Time{}},
			},
			listParams: nil,
			downloads:  nil,
		}
		source := tc.newSource(cfg, client)

		upperBound, err := source.UpperBound(t.Context(), endTime)

		require.NoError(t, err, tc.name)
		require.Equal(t, start, upperBound, tc.name)
		require.Len(t, client.listParams, 1, tc.name)
		require.Equal(t, tc.eventType, client.listParams[0].EventType, tc.name)
		require.Equal(t, start, client.listParams[0].After, tc.name)
	}
}

func TestLogFileSourcesUpperBoundRejectsNonAdvancingLastEndTime(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 7, 27, 10, 0, 0, 123456000, time.UTC)
	for _, tc := range logFileSourceCases() {
		cfg := tc.newConfig()
		cfg.PollWatermarkAt = start
		cfg.PollCheckpoint = timewindowpoller.CompletedCheckpoint(start)
		client := &stubCodexComplianceClient{
			listPages: []*codexapi.LogsPage{
				{Data: nil, HasMore: true, LastEndTime: start},
			},
			listParams: nil,
			downloads:  nil,
		}
		source := tc.newSource(cfg, client)

		_, err := source.UpperBound(t.Context(), start.Add(time.Hour))

		require.Error(t, err, tc.name)
		require.Contains(t, err.Error(), "last_end_time did not advance", tc.name)
		require.Len(t, client.listParams, 1, tc.name)
	}
}

func TestLogFileSourcesFetchPageStopsAtWindowEndAndFiltersForeignTypes(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)
	end := time.Date(2026, 7, 27, 11, 0, 0, 0, time.UTC)
	for _, tc := range logFileSourceCases() {
		inWindow := codexapi.LogFile{ID: "eclf_in", EventType: tc.eventType, EndTime: start.Add(30 * time.Minute), FileName: "", FileSize: 0, FileSHA256: ""}
		afterWindow := codexapi.LogFile{ID: "eclf_after", EventType: tc.eventType, EndTime: end.Add(time.Minute), FileName: "", FileSize: 0, FileSHA256: ""}
		foreign := codexapi.LogFile{ID: "eclf_foreign", EventType: "AUDIT_LOG", EndTime: start.Add(20 * time.Minute), FileName: "", FileSize: 0, FileSHA256: ""}
		client := &stubCodexComplianceClient{
			listPages: []*codexapi.LogsPage{
				{Data: []codexapi.LogFile{foreign, inWindow, afterWindow}, HasMore: true, LastEndTime: afterWindow.EndTime},
			},
			listParams: nil,
			downloads:  nil,
		}
		source := tc.newSource(tc.newConfig(), client)

		page, err := source.FetchPage(t.Context(), start, end, "")

		require.NoError(t, err, tc.name)
		// The out-of-window file forces window truncation: no next page.
		require.False(t, page.HasMore, tc.name)
		require.Empty(t, page.NextPage, tc.name)
		// The foreign-type file is filtered; only the in-window own-type file
		// survives.
		require.Equal(t, []codexapi.LogFile{inWindow}, page.Payload, tc.name)
	}
}

func TestLogFileSourcesFetchPageRejectsNonAdvancingLastEndTime(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 7, 27, 10, 0, 0, 123456000, time.UTC)
	end := start.Add(time.Hour)
	for _, tc := range logFileSourceCases() {
		client := &stubCodexComplianceClient{
			listPages: []*codexapi.LogsPage{
				{Data: nil, HasMore: true, LastEndTime: start},
			},
			listParams: nil,
			downloads:  nil,
		}
		source := tc.newSource(tc.newConfig(), client)

		page, err := source.FetchPage(t.Context(), start, end, "")

		require.Error(t, err, tc.name)
		require.Contains(t, err.Error(), "last_end_time did not advance", tc.name)
		require.False(t, page.HasMore, tc.name)
		require.Nil(t, page.Payload, tc.name)
	}
}
