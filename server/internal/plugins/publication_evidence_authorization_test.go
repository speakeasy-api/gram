package plugins_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestPublicationEvidenceRequiresOrganizationAdmin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	_, err := ti.service.ResolvePublicationEvidence(context.Background(), ac.ActiveOrganizationID, *ac.ProjectID, nil)
	require.Error(t, err, "a caller without prepared grants must not read package addresses")
	_, err = ti.service.ResolvePublicationEvidence(authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeOrgRead, ac.ActiveOrganizationID)}), ac.ActiveOrganizationID, *ac.ProjectID, nil)
	require.Error(t, err, "organization readers must not read admin package evidence")
	_, err = ti.service.ResolvePublicationEvidence(ctx, ac.ActiveOrganizationID, uuid.New(), nil)
	require.Error(t, err, "admin access does not bypass project ownership")
}
