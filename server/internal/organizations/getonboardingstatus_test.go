package organizations_test

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func TestService_GetOnboardingStatusPersistsLiveVerifiedDomains(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	org, err := orgrepo.New(ti.conn).GetOrganizationMetadata(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.True(t, org.WorkosID.Valid)
	require.Empty(t, org.VerifiedDomains)

	workosOrgID := org.WorkosID.String
	ti.orgs.On("GetOrganizationDomainPolicy", mock.Anything, workosOrgID).Return(&thirdpartyworkos.OrganizationDomainPolicy{
		Domains: []thirdpartyworkos.OrganizationDomain{
			{Domain: "pending.example.com", State: thirdpartyworkos.OrganizationDomainStatePending},
			{Domain: "example.com", State: thirdpartyworkos.OrganizationDomainStateVerified},
			{Domain: "legacy.example.com", State: thirdpartyworkos.OrganizationDomainStateLegacyVerified},
		},
	}, nil).Once()
	ti.orgs.On("ListConnections", mock.Anything, workosOrgID).Return([]thirdpartyworkos.Connection{}, nil).Twice()
	ti.orgs.On("ListDirectories", mock.Anything, workosOrgID).Return([]thirdpartyworkos.Directory{}, nil).Twice()

	result, err := ti.service.GetOnboardingStatus(ctx, &gen.GetOnboardingStatusPayload{})
	require.NoError(t, err)
	require.True(t, result.DomainVerified)
	require.Equal(t, []string{"example.com", "legacy.example.com"}, result.VerifiedDomains)
	require.False(t, result.SsoConfigured)
	require.False(t, result.DsyncConfigured)

	org, err = orgrepo.New(ti.conn).GetOrganizationMetadata(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.Equal(t, []string{"example.com", "legacy.example.com"}, org.VerifiedDomains)

	// A stored non-empty list answers without asking WorkOS again: the domain
	// policy expectation above is Once, so a second lookup would fail the mock.
	result, err = ti.service.GetOnboardingStatus(ctx, &gen.GetOnboardingStatusPayload{})
	require.NoError(t, err)
	require.True(t, result.DomainVerified)
	require.Equal(t, []string{"example.com", "legacy.example.com"}, result.VerifiedDomains)
}

func TestService_GetOnboardingStatusReportsUnverifiedDomain(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	org, err := orgrepo.New(ti.conn).GetOrganizationMetadata(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.True(t, org.WorkosID.Valid)

	workosOrgID := org.WorkosID.String
	ti.orgs.On("GetOrganizationDomainPolicy", mock.Anything, workosOrgID).Return(&thirdpartyworkos.OrganizationDomainPolicy{
		Domains: []thirdpartyworkos.OrganizationDomain{
			{Domain: "example.com", State: thirdpartyworkos.OrganizationDomainStatePending},
		},
	}, nil).Once()
	ti.orgs.On("ListConnections", mock.Anything, workosOrgID).Return([]thirdpartyworkos.Connection{}, nil).Once()
	ti.orgs.On("ListDirectories", mock.Anything, workosOrgID).Return([]thirdpartyworkos.Directory{}, nil).Once()

	result, err := ti.service.GetOnboardingStatus(ctx, &gen.GetOnboardingStatusPayload{})
	require.NoError(t, err)
	require.False(t, result.DomainVerified)
	require.Empty(t, result.VerifiedDomains)
	require.NotNil(t, result.VerifiedDomains)

	org, err = orgrepo.New(ti.conn).GetOrganizationMetadata(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.Empty(t, org.VerifiedDomains)
}
