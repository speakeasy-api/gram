package platformmcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// A missing or partly composed plugin service answers create, rename, and the
// unconfirmed preview with the unavailable refusal. Logging that refusal must
// not dereference the service it is reporting as missing.
func TestPluginMetadataUncomposedServiceRefusesWithoutPanicking(t *testing.T) {
	t.Parallel()

	principal := Principal{OrganizationID: "org-placeholder", UserID: "user-placeholder"}
	services := map[string]*PluginsService{
		"nil":          nil,
		"not composed": NewPluginsService(nil, testOperationBudget(), "plugin-metadata-cursor-key"),
	}
	for name, service := range services {
		calls := map[string]func() error{
			"create": func() error {
				_, err := service.CreatePlugin(t.Context(), principal, CreatePluginInput{
					ProjectID: uuid.NewString(), Name: "Plugin", IdempotencyKey: "key", Confirmed: true,
				})
				return err
			},
			"preview": func() error {
				_, err := service.CreatePlugin(t.Context(), principal, CreatePluginInput{
					ProjectID: uuid.NewString(), Name: "Plugin", IdempotencyKey: "key", Confirmed: false,
				})
				return err
			},
			"rename": func() error {
				_, err := service.RenamePlugin(t.Context(), principal, RenamePluginInput{
					ProjectID: uuid.NewString(), Plugin: "plugin", Name: "Renamed", IdempotencyKey: "key", Confirmed: true,
				})
				return err
			},
		}
		for call, invoke := range calls {
			var err error
			require.NotPanics(t, func() { err = invoke() }, "%s on a %s service", call, name)
			var refusal *PluginMetadataMutationError
			require.ErrorAs(t, err, &refusal, "%s on a %s service", call, name)
			require.Equal(t, unavailableCode, refusal.Code, "%s on a %s service", call, name)
		}
	}
}
