package aiintegrations

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/attr"
	cursorapi "github.com/speakeasy-api/gram/server/internal/thirdparty/cursor"
)

func TestBuildCursorUsageEventIncludesIntegrationConfigID(t *testing.T) {
	t.Parallel()

	configID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	cfg := Config{
		ID:             configID,
		OrganizationID: "org_123",
		Provider:       ProviderCursor,
		ProjectID:      uuid.MustParse("22222222-2222-2222-2222-222222222222"),
	}
	event := cursorapi.UsageEvent{
		Timestamp: time.Date(2026, 5, 20, 12, 30, 0, 0, time.UTC),
		Model:     "claude-4",
		Kind:      "usage",
		TokenUsage: cursorapi.TokenUsage{
			InputTokens:      10,
			OutputTokens:     20,
			CacheReadTokens:  30,
			CacheWriteTokens: 40,
			TotalCents:       50,
		},
		UserEmail: "User@Example.com",
	}

	logParam := (&UsagePollService{}).buildCursorUsageEvent(cfg, event)

	require.Equal(t, configID.String(), logParam.Attributes[attr.AIIntegrationConfigIDKey])
	require.Equal(t, "cursor", logParam.Attributes[attr.HookSourceKey])
}
