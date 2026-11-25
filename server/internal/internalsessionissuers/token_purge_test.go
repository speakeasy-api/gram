package internalsessionissuers_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/internal_session_issuers"
)

type recordingIssuerTokenPurger struct {
	organizations []string
}

func (p *recordingIssuerTokenPurger) PurgeEMAIssuerTokens(_ context.Context, organizationID string) error {
	p.organizations = append(p.organizations, organizationID)
	return nil
}

func TestContainmentPurgesTokensWithoutProvisioner(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"disable", "compromised_rotation"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t, false)
			provisionIssuer(t, ctx, ti)
			purger := &recordingIssuerTokenPurger{}
			ti.service.SetEMAIssuerTokenPurger(purger)
			admin := adminCtx(t, ctx)
			if action == "disable" {
				_, err := ti.service.SetEnabled(admin, &gen.SetEnabledPayload{Enabled: false, SessionToken: nil})
				require.NoError(t, err)
			} else {
				view, err := ti.service.Get(admin, &gen.GetPayload{SessionToken: nil})
				require.NoError(t, err)
				require.NotEmpty(t, view.Keys)
				_, err = ti.service.Rotate(admin, &gen.RotatePayload{ActiveKid: &view.Keys[0].Kid, Compromised: true, SessionToken: nil})
				require.NoError(t, err)
			}
			require.Equal(t, []string{ti.orgID}, purger.organizations)
		})
	}
}
