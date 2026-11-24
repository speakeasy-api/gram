package gram

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
)

// The checked-in flags.csv opens connection health for every local caller,
// whether evaluated by email (a session) or by organization (an API key).
func TestCheckedInFlagsOpenConnectionHealthForEveryCaller(t *testing.T) {
	t.Chdir(filepath.Join("..", ".."))
	wd, err := os.Getwd()
	require.NoError(t, err)
	flags := newLocalFeatureFlags(t.Context(), slog.New(slog.DiscardHandler), filepath.Join(wd, "flags.csv"))
	groups := feature.OrgProjectGroups("local-dev-org", "default")

	on, err := flags.IsFlagEnabledLocal(t.Context(), feature.FlagMCPConnectionHealth, "dev@example.com", groups, map[string]string{"email": "dev@example.com"})
	require.NoError(t, err)
	require.True(t, on)

	evaluation, err := feature.EvaluateFlag(t.Context(), flags, feature.FlagMCPConnectionHealth, "72640a3e-93dd-5dbc-89b2-6b95bead0fac", groups)
	require.NoError(t, err)
	require.Equal(t, feature.EvaluationEnabled, evaluation)
}
