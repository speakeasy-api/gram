package signins

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestRecordExchange_SkipsWithoutTouchingTheDatabase(t *testing.T) {
	t.Parallel()

	for _, p := range []ExchangeParams{
		{Flow: FlowMCPAuthorize, WorkOSOrganizationID: "org_1", ExpectedOrganizationID: "o1", UserID: "u1", WorkOSSessionID: "s1", AuthenticationMethod: MethodSSO, Impersonated: true},
		{Flow: FlowDashboard, WorkOSOrganizationID: "org_1", ExpectedOrganizationID: "", UserID: "u1", WorkOSSessionID: "", AuthenticationMethod: MethodSSO, Impersonated: false},
		{Flow: FlowDashboard, WorkOSOrganizationID: "", ExpectedOrganizationID: "", UserID: "u1", WorkOSSessionID: "s1", AuthenticationMethod: MethodSSO, Impersonated: false},
	} {
		got := RecordExchange(t.Context(), testenv.NewLogger(t), nil, p)
		require.False(t, got.ID.Valid)
	}
}
