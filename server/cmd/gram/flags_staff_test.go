package gram

import (
	"flag"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/staff"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func staffFlagContext(t *testing.T, environment, staffOrganizationID string) *cli.Context {
	t.Helper()

	flags := flag.NewFlagSet("staff", flag.ContinueOnError)
	flags.String("environment", environment, "")
	flags.String(staffOrganizationIDFlag, staffOrganizationID, "")
	return cli.NewContext(nil, flags, nil)
}

func TestStaffOrganizationFromCLIRequiresValueOutsideLocal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		environment string
		raw         string
	}{
		{name: "dev empty", environment: "dev", raw: ""},
		{name: "prod empty", environment: "prod", raw: ""},
		{name: "prod blank", environment: "prod", raw: "  "},
		{name: "test empty", environment: "test", raw: ""},
		{name: "unset environment", environment: "", raw: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := staffOrganizationFromCLI(staffFlagContext(t, tt.environment, tt.raw))
			require.ErrorContains(t, err, "--staff-organization-id is required")
		})
	}
}

func TestStaffOrganizationFromCLIOptionalInLocal(t *testing.T) {
	t.Parallel()

	staffOrg, err := staffOrganizationFromCLI(staffFlagContext(t, "local", ""))
	require.NoError(t, err)
	require.False(t, staffOrg.Configured())
}

func TestStaffOrganizationFromCLITrimsConfiguredValue(t *testing.T) {
	t.Parallel()

	staffOrg, err := staffOrganizationFromCLI(staffFlagContext(t, "prod", " org-staff "))
	require.NoError(t, err)
	require.Equal(t, "org-staff", staffOrg.ID())
}

// Every deployed command that builds a staff-aware component declares the flag,
// so a value set once in the shared environment reaches all of them.
func TestStaffFlagsDeclaredByStaffAwareCommands(t *testing.T) {
	t.Parallel()

	for _, command := range []*cli.Command{
		newStartCommand(),
		newWorkerCommand(),
		newStreamsCommand(),
		newMCPCommand(),
		newAdminCommand(),
		newNetworkIngressServerCommand(),
		newDemoSeedCommand(),
	} {
		names := make([]string, 0, len(command.Flags))
		for _, f := range command.Flags {
			names = append(names, f.Names()...)
		}
		require.True(t, slices.Contains(names, staffOrganizationIDFlag), "%s must declare --%s", command.Name, staffOrganizationIDFlag)
	}
}

func TestVerifyStaffOrganizationRequiresExistingOrganization(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	infra, cleanup, err := testenv.Launch(ctx, testenv.LaunchOptions{Postgres: true})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	pool, err := infra.CloneTestDatabase(t, "verify_staff_organization")
	require.NoError(t, err)

	organizationID := "org_" + uuid.NewString()
	_, err = organizationsrepo.New(pool).UpsertOrganizationMetadata(ctx, organizationsrepo.UpsertOrganizationMetadataParams{ID: organizationID, Name: "Staff", Slug: "staff-" + uuid.NewString()})
	require.NoError(t, err)

	require.NoError(t, verifyStaffOrganization(ctx, pool, staff.NewOrganization(organizationID)))
	require.NoError(t, verifyStaffOrganization(ctx, pool, staff.Organization{}))

	err = verifyStaffOrganization(ctx, pool, staff.NewOrganization("speakeasy-team"))
	require.ErrorContains(t, err, "matches no organization")
}
