package gram

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/urfave/cli/v2"

	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/staff"
)

const staffOrganizationIDFlag = "staff-organization-id"

// staffOrganizationOptionalEnvironments are the only environments where a
// process may start without a staff organization. Elsewhere an unset value
// would silently turn off staff masking in customer audit feeds, so the
// process refuses to start instead. Listing the exemptions means a new or
// misspelled environment fails safe. A local stack has no staff organization.
var staffOrganizationOptionalEnvironments = []string{"local"}

// staffFlags configure the deployment's Speakeasy staff organization. Every
// command that builds a staff-aware component declares them.
func staffFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:     staffOrganizationIDFlag,
			Usage:    "ID of this deployment's Speakeasy staff organization (speakeasy-team in the US). Customer audit feeds mask its members, and it bypasses standard OpenRouter credit limits, gets new hooks releases first, and may set organization whitelist status. Required in every environment except local.",
			EnvVars:  []string{"GRAM_STAFF_ORGANIZATION_ID"},
			Required: false,
		},
	}
}

// staffOrganizationFromCLI reads the staff organization flag. It returns an
// error when the flag is empty outside the environments where it is optional.
func staffOrganizationFromCLI(c *cli.Context) (staff.Organization, error) {
	id := strings.TrimSpace(c.String(staffOrganizationIDFlag))
	environment := c.String("environment")
	if id == "" && !slices.Contains(staffOrganizationOptionalEnvironments, environment) {
		return staff.Organization{}, fmt.Errorf("--%s is required in environment %q", staffOrganizationIDFlag, environment)
	}

	return staff.NewOrganization(id), nil
}

// verifyStaffOrganization fails when the configured staff organization has no
// row in this deployment's database. A slug or another partition's ID passes
// staffOrganizationFromCLI but matches no organization, which would silently
// turn off staff masking, so the process refuses to start instead. It does
// nothing when no staff organization is configured.
func verifyStaffOrganization(ctx context.Context, db orgrepo.DBTX, staffOrg staff.Organization) error {
	if !staffOrg.Configured() {
		return nil
	}

	_, err := orgrepo.New(db).GetOrganizationMetadata(ctx, staffOrg.ID())
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("--%s %q matches no organization in this deployment's database", staffOrganizationIDFlag, staffOrg.ID())
	case err != nil:
		return fmt.Errorf("read staff organization: %w", err)
	default:
		return nil
	}
}
