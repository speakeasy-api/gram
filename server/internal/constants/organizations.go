package constants

// USSpeakeasyTeamOrganizationID is the speakeasy-team organization in the US
// production database. Other partitions and environments have no row with
// this ID, so only US-specific data and tooling defaults may use it. Staff
// behavior uses the configured staff organization (staff.Organization).
const USSpeakeasyTeamOrganizationID = "5a25158b-24dc-4d49-b03d-e85acfbea59c"
