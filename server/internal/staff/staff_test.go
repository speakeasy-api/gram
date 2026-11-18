package staff_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/staff"
)

func TestOrganizationIsMatchesConfiguredID(t *testing.T) {
	t.Parallel()

	org := staff.NewOrganization("org-staff")

	require.True(t, org.Configured())
	require.Equal(t, "org-staff", org.ID())
	require.True(t, org.Is("org-staff"))
	require.False(t, org.Is("org-customer"))
	require.False(t, org.Is(""))
}

func TestOrganizationUnconfiguredMatchesNothing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		org  staff.Organization
	}{
		{name: "empty ID", org: staff.NewOrganization("")},
		{name: "zero value", org: staff.Organization{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.False(t, tt.org.Configured())
			require.Empty(t, tt.org.ID())
			require.False(t, tt.org.Is(""))
			require.False(t, tt.org.Is("org-customer"))
		})
	}
}
