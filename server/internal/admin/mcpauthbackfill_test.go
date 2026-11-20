package admin

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestBackfillMcpAuthGrantsRetired(t *testing.T) {
	t.Parallel()
	svc := &Service{}
	payload := &gen.BackfillMcpAuthGrantsPayload{}
	_, err := svc.BackfillMcpAuthGrants(t.Context(), payload)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeUnauthorized, shareable.Code)

	ctx := contextvalues.SetAdminAuthContext(t.Context(), &contextvalues.AdminAuthContext{OIDCSubject: "staff-test"})
	_, err = svc.BackfillMcpAuthGrants(ctx, payload)
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeNotImplemented, shareable.Code)
	require.ErrorContains(t, err, "the upstream auth grant backfill has been retired")
}
