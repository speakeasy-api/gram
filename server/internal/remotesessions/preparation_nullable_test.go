package remotesessions

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/stretchr/testify/require"
)

func TestPreparationNullableBindingFields(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name       string
		state      pgtype.Text
		source     pgtype.Text
		wantState  string
		wantSource string
	}{
		{"null", pgtype.Text{}, pgtype.Text{}, "configuration_required", "unknown"},
		{"invalid ignores payload", pgtype.Text{String: "ready", Valid: false}, pgtype.Text{String: "provider_returned", Valid: false}, "configuration_required", "unknown"},
		{"ready", conv.ToPGText("ready"), conv.ToPGText("provider_returned"), "ready", "provider_returned"},
		{"unlinked", conv.ToPGText("unlinked"), conv.ToPGText("unknown"), "unlinked", "unknown"},
		{"published", conv.ToPGText("published_acceptance_unverified"), conv.ToPGText("cimd_published"), "published_acceptance_unverified", "cimd_published"},
		{"null state only", pgtype.Text{}, conv.ToPGText("administrator_declared"), "configuration_required", "administrator_declared"},
		{"null source only", conv.ToPGText("in_progress"), pgtype.Text{}, "in_progress", "unknown"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			binding := repo.RemoteSessionEmaBinding{State: tt.state, GrantSource: tt.source}
			require.Equal(t, tt.wantState, preparationBindingState(binding.State))
			require.Equal(t, tt.wantSource, preparationBindingGrantSource(binding.GrantSource))
			result := preparationResult(binding, repo.RemoteSessionIssuer{}, repo.RemoteSessionClient{}, preparationBindingState(binding.State))
			require.Equal(t, tt.wantState, result.State)
			require.Equal(t, tt.wantSource, result.GrantSource)
			if tt.wantState == "configuration_required" {
				require.NotEmpty(t, result.Remediation)
				require.False(t, result.Retryable)
			}
		})
	}
}
