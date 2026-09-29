package admin

import (
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
)

const workosDashboardOrigin = "https://dashboard.workos.com"

// SetWorkOSEnvironmentID sets the WorkOS environment that organization
// dashboard links are scoped to. WorkOS dashboard URLs need it and no WorkOS
// API reachable with the deployment's API key returns it, so it is configured.
func (s *Service) SetWorkOSEnvironmentID(environmentID string) {
	s.workosEnvironmentID = strings.TrimSpace(environmentID)
}

// workosDashboardURL links to an organization in the WorkOS dashboard. It is
// nil unless both the environment and the organization's WorkOS ID are known.
func (s *Service) workosDashboardURL(workosID pgtype.Text) *string {
	if s.workosEnvironmentID == "" || !workosID.Valid || workosID.String == "" {
		return nil
	}
	link := workosDashboardOrigin + "/" + url.PathEscape(s.workosEnvironmentID) + "/organizations/" + url.PathEscape(workosID.String)
	return &link
}
