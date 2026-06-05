package platformtools

import (
	"bytes"
	"context"
	"testing"

	"github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/platformtools/logs"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/stretchr/testify/require"
)

type fakeTelemetryService struct {
	payload *telemetry.SearchLogsPayload
}

func (f *fakeTelemetryService) SearchLogs(_ context.Context, payload *telemetry.SearchLogsPayload) (*telemetry.SearchLogsResult, error) {
	f.payload = payload
	return &telemetry.SearchLogsResult{}, nil
}

func (f *fakeTelemetryService) SearchToolCalls(_ context.Context, _ *telemetry.SearchToolCallsPayload) (*telemetry.SearchToolCallsResult, error) {
	return &telemetry.SearchToolCallsResult{}, nil
}

func (f *fakeTelemetryService) SearchChats(_ context.Context, _ *telemetry.SearchChatsPayload) (*telemetry.SearchChatsResult, error) {
	return &telemetry.SearchChatsResult{}, nil
}

func (f *fakeTelemetryService) SearchUsers(_ context.Context, _ *telemetry.SearchUsersPayload) (*telemetry.SearchUsersResult, error) {
	return &telemetry.SearchUsersResult{}, nil
}

func (f *fakeTelemetryService) GetProjectMetricsSummary(_ context.Context, _ *telemetry.GetProjectMetricsSummaryPayload) (*telemetry.GetMetricsSummaryResult, error) {
	return &telemetry.GetMetricsSummaryResult{}, nil
}

func (f *fakeTelemetryService) GetUserMetricsSummary(_ context.Context, _ *telemetry.GetUserMetricsSummaryPayload) (*telemetry.GetUserMetricsSummaryResult, error) {
	return &telemetry.GetUserMetricsSummaryResult{}, nil
}

func (f *fakeTelemetryService) GetObservabilityOverview(_ context.Context, _ *telemetry.GetObservabilityOverviewPayload) (*telemetry.GetObservabilityOverviewResult, error) {
	return &telemetry.GetObservabilityOverviewResult{}, nil
}

func (f *fakeTelemetryService) ListAttributeKeys(_ context.Context, _ *telemetry.ListAttributeKeysPayload) (*telemetry.ListAttributeKeysResult, error) {
	return &telemetry.ListAttributeKeysResult{}, nil
}

func TestExecuteSearchLogs_IgnoresInjectedAuthFields(t *testing.T) {
	t.Parallel()

	fakeTelemetry := &fakeTelemetryService{}
	tool := logs.NewSearchLogsTool(fakeTelemetry)

	from := "2026-04-08T09:00:00Z"
	var out bytes.Buffer
	err := tool.Call(t.Context(), toolconfig.ToolCallEnv{
		UserConfig: toolconfig.NewCaseInsensitiveEnv(),
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "",
		GramEmail:  "",
	}, bytes.NewBufferString(`{
		"apikeyToken":"api-key",
		"sessionToken":"session",
		"projectSlugInput":"other-project",
		"from":"`+from+`",
		"limit":25,
		"sort":"asc"
	}`), &out)
	require.NoError(t, err)

	require.NotNil(t, fakeTelemetry.payload)
	require.Nil(t, fakeTelemetry.payload.ApikeyToken)
	require.Nil(t, fakeTelemetry.payload.SessionToken)
	require.Nil(t, fakeTelemetry.payload.ProjectSlugInput)
	require.NotNil(t, fakeTelemetry.payload.From)
	require.Equal(t, from, *fakeTelemetry.payload.From)
	require.Equal(t, 25, fakeTelemetry.payload.Limit)
	require.Equal(t, "asc", fakeTelemetry.payload.Sort)
}
