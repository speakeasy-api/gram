package orgprovision_test

import (
	"testing"

	"github.com/speakeasy-api/gram/dev-idp/pkg/devidptest"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/stretchr/testify/require"
)

func newEmulatorClient(t *testing.T) *workos.Client {
	t.Helper()
	idp := devidptest.Launch(t, devidptest.LaunchOpts{EnableWorkOS: true})
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	return workos.NewClient(policy, "dev-idp-mock", workos.ClientOpts{Endpoint: idp.WorkOSURL, ClientID: "dev-idp-mock"})
}
