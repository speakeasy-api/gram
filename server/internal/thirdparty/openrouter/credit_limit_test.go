package openrouter

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/staff"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestDefaultCreditLimitStaffOrganizationBypassesTierPolicy(t *testing.T) {
	t.Parallel()

	staffOrg := staff.NewOrganization("org-staff")
	for _, activeTrial := range []bool{false, true} {
		limit, ok := DefaultCreditLimit(staffOrg, "org-staff", billing.TierBase, activeTrial)
		require.True(t, ok)
		require.Equal(t, staffOrganizationCreditLimit, limit)
	}

	limit, ok := DefaultCreditLimit(staffOrg, "org-customer", billing.TierBase, false)
	require.True(t, ok)
	expected, ok := AccountTypeCreditLimit(billing.TierBase)
	require.True(t, ok)
	require.Equal(t, expected, limit)
}

func TestDefaultCreditLimitUnconfiguredStaffOrganizationExemptsNobody(t *testing.T) {
	t.Parallel()

	limit, ok := DefaultCreditLimit(staff.Organization{}, "", billing.TierBase, true)
	require.True(t, ok)
	require.Equal(t, trialCreditLimit, limit)
}

// The provisioner hands its configured staff organization to the policy, so the
// staff organization's keys mint at the staff limit. The staff check resolves
// before any trial read, so no database is needed.
func TestDefaultLimitForOrgUsesConfiguredStaffOrganization(t *testing.T) {
	t.Parallel()

	provisioner := New(testenv.NewLogger(t), testenv.NewTracerProvider(t), guardian.NewDefaultPolicy(testenv.NewTracerProvider(t)), nil, "test", "provisioning-key", nil, nil, nil, staff.NewOrganization("org-staff"))

	limit := provisioner.defaultLimitForOrg(t.Context(), nil, orgRepo.OrganizationMetadatum{ID: "org-staff", GramAccountType: string(billing.TierBase)})
	require.Equal(t, staffOrganizationCreditLimit, limit)
}
