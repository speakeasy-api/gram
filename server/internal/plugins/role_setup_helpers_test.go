package plugins_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/roledistribution"
	"github.com/stretchr/testify/require"
)

type roleSetupTarget struct {
	roleURN        string
	organizationID string
}

// Resolve only the fixture's known role. Production dispatch supplies this
// identity from its message; tests never discover or sweep pending work.
func roleSetupIdentity(t *testing.T, ctx context.Context, ti *testInstance, role string) roleSetupTarget {
	t.Helper()
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	target := roleSetupTarget{roleURN: role, organizationID: ac.ActiveOrganizationID}
	return target
}

func processRoleSetup(ctx context.Context, ti *testInstance, target roleSetupTarget, publication plugins.PublicationRequests) (int, error) {
	changed, err := roledistribution.ProcessRoleDistributionSetup(ctx, ti.conn, publication, nil, target.roleURN, target.organizationID)
	if err != nil {
		return 0, fmt.Errorf("process fixture role distribution: %w", err)
	}
	if changed {
		return 1, nil
	}
	return 0, nil
}

func runRoleSetup(t *testing.T, ctx context.Context, ti *testInstance, role string) int {
	t.Helper()
	n, err := processRoleSetup(ctx, ti, roleSetupIdentity(t, ctx, ti, role), plugins.PublicationRequests{Enabled: true})
	require.NoError(t, err)
	return n
}
