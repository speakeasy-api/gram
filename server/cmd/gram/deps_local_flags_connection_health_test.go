package gram

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
)

// The checked-in flags.csv must open the connection health gate for a
// signed-in developer and for an API key, as the local dashboard does.
func TestCheckedInFlagsOpenConnectionHealthForSessionsAndKeys(t *testing.T) {
	t.Chdir(filepath.Join("..", ".."))
	wd, err := os.Getwd()
	require.NoError(t, err)
	flags := newLocalFeatureFlags(t.Context(), slog.New(slog.DiscardHandler), filepath.Join(wd, "flags.csv"))

	const organizationID = "72640a3e-93dd-5dbc-89b2-6b95bead0fac"
	authCtx := func() *contextvalues.AuthContext {
		return &contextvalues.AuthContext{ActiveOrganizationID: organizationID, UserID: "user", Email: new("dev@example.com")}
	}
	groups := feature.OrgProjectGroups("local-dev-org", "default")
	for name, ctx := range map[string]func() (string, map[string]string, bool){
		"session": func() (string, map[string]string, bool) {
			return feature.FlagTarget(contextvalues.WithValidatedGramSession(t.Context(), authCtx(), false), organizationID)
		},
		"api key": func() (string, map[string]string, bool) {
			return feature.FlagTarget(contextvalues.WithLegacyAPIKeyAuthorization(t.Context(), authCtx()), organizationID)
		},
	} {
		distinctID, person, _ := ctx()
		evaluation, err := feature.EvaluateFlag(t.Context(), flags, feature.FlagMCPConnectionHealth, distinctID, groups, person)
		require.NoError(t, err, name)
		require.Equal(t, feature.EvaluationEnabled, evaluation, name)
	}
}
