package growthsignals_test

import (
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/growthsignals"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestBuildEventCarriesEveryProperty(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("11111111-2222-3333-4444-555555555555")

	built := growthsignals.BuildEvent(growthsignals.ActivityEvent{
		Activity:       growthsignals.ActivityMcpServerCreated,
		OrganizationID: "org_placeholder",
		ProjectID:      projectID,
		ActorID:        "user_placeholder",
		ActorType:      urn.PrincipalTypeUser,
		ActorEmail:     "",
		ActorName:      "Acting Person",
		SubjectName:    "Widget Server",
		ActingSurface:  string(audit.SurfaceDashboard),
		AuditAction:    audit.ActionMcpServerCreate,
		DashboardURL:   "https://app.example.test/acme/widgets/mcp/widget-server",
		Extra:          map[string]string{growthsignals.PropertyMcpKind: string(growthsignals.McpKindHosted)},
	}, growthsignals.Enrichment{
		Organization: growthsignals.OrganizationDetails{Slug: "acme", Name: "Acme Incorporated"},
		Project:      growthsignals.ProjectDetails{Slug: "widgets", Name: "Widgets"},
		ActorEmail:   "person@example.test",
	}, testSiteURL())

	require.Equal(t, growthsignals.EventName, built.Name)
	require.Equal(t, "person@example.test", built.DistinctID)
	require.Equal(t, map[string]any{
		"activity":          "mcp_server_created",
		"organization_id":   "org_placeholder",
		"organization_slug": "acme",
		"organization_name": "Acme Incorporated",
		"project_id":        projectID.String(),
		"project_slug":      "widgets",
		"project_name":      "Widgets",
		"actor_email":       "person@example.test",
		"actor_name":        "Acting Person",
		"subject_name":      "Widget Server",
		"acting_surface":    "dashboard",
		"dashboard_url":     "https://app.example.test/acme/widgets/mcp/widget-server",
		"audit_action":      "mcp-server:create",
		"mcp_kind":          "hosted",
	}, built.Properties)
}

// A blank property in Slack reads as a thing with no name; an absent one reads
// as what it is.
func TestBuildEventOmitsUnresolvedProperties(t *testing.T) {
	t.Parallel()

	built := growthsignals.BuildEvent(growthsignals.ActivityEvent{
		Activity:       growthsignals.ActivityProjectCreated,
		OrganizationID: "org_placeholder",
		ProjectID:      uuid.Nil,
		ActorID:        "",
		ActorType:      "",
		ActorEmail:     "",
		ActorName:      "",
		SubjectName:    "",
		ActingSurface:  "",
		AuditAction:    "",
		DashboardURL:   "",
		Extra:          map[string]string{growthsignals.PropertyRole: ""},
	}, growthsignals.Enrichment{
		Organization: growthsignals.OrganizationDetails{Slug: "", Name: ""},
		Project:      growthsignals.ProjectDetails{Slug: "", Name: ""},
		ActorEmail:   "",
	}, testSiteURL())

	require.Equal(t, map[string]any{
		"activity":        "project_created",
		"organization_id": "org_placeholder",
		// dashboard_url is never omitted: with no subject page and no resolved
		// organization slug it falls back to the site root.
		"dashboard_url": "https://app.example.test",
	}, built.Properties)
}

// Project slug and name describe a project, so they must not appear on an
// organization-scoped activity even when a stale enrichment carries them.
func TestBuildEventOmitsProjectPropertiesWhenNotProjectScoped(t *testing.T) {
	t.Parallel()

	built := growthsignals.BuildEvent(growthsignals.ActivityEvent{
		Activity:       growthsignals.ActivityMemberInvited,
		OrganizationID: "org_placeholder",
		ProjectID:      uuid.Nil,
		ActorID:        "",
		ActorType:      "",
		ActorEmail:     "",
		ActorName:      "",
		SubjectName:    "",
		ActingSurface:  "",
		AuditAction:    "",
		DashboardURL:   "",
		Extra:          nil,
	}, growthsignals.Enrichment{
		Organization: growthsignals.OrganizationDetails{Slug: "acme", Name: "Acme Incorporated"},
		Project:      growthsignals.ProjectDetails{Slug: "widgets", Name: "Widgets"},
		ActorEmail:   "",
	}, testSiteURL())

	require.NotContains(t, built.Properties, "project_id")
	require.NotContains(t, built.Properties, "project_slug")
	require.NotContains(t, built.Properties, "project_name")
}

// Role and system actors have no person behind them, so the organization is the
// only sensible thing left to attach the event to.
func TestBuildEventDistinctIDFallsBackToOrganization(t *testing.T) {
	t.Parallel()

	built := growthsignals.BuildEvent(growthsignals.ActivityEvent{
		Activity:       growthsignals.ActivitySecurityPolicyUpdated,
		OrganizationID: "org_placeholder",
		ProjectID:      uuid.Nil,
		ActorID:        "admin",
		ActorType:      urn.PrincipalTypeRole,
		ActorEmail:     "",
		ActorName:      "",
		SubjectName:    "",
		ActingSurface:  "",
		AuditAction:    "",
		DashboardURL:   "",
		Extra:          nil,
	}, growthsignals.Enrichment{
		Organization: growthsignals.OrganizationDetails{Slug: "", Name: ""},
		Project:      growthsignals.ProjectDetails{Slug: "", Name: ""},
		ActorEmail:   "",
	}, testSiteURL())

	require.Equal(t, "org_placeholder", built.DistinctID)
	require.NotContains(t, built.Properties, "actor_email")
}

// Extras are supplied per activity, so a stray key must not be able to rewrite
// the identity of the event carrying it.
func TestBuildEventBasePropertiesWinOverExtras(t *testing.T) {
	t.Parallel()

	built := growthsignals.BuildEvent(growthsignals.ActivityEvent{
		Activity:       growthsignals.ActivityUserSignedUp,
		OrganizationID: "org_placeholder",
		ProjectID:      uuid.Nil,
		ActorID:        "",
		ActorType:      "",
		ActorEmail:     "",
		ActorName:      "",
		SubjectName:    "",
		ActingSurface:  "",
		AuditAction:    "",
		DashboardURL:   "",
		Extra: map[string]string{
			"activity":                         "something_else",
			"organization_id":                  "org_other",
			growthsignals.PropertySignupSource: growthsignals.SignupSourceInvited,
			"":                                 "blank key",
		},
	}, growthsignals.Enrichment{
		Organization: growthsignals.OrganizationDetails{Slug: "", Name: ""},
		Project:      growthsignals.ProjectDetails{Slug: "", Name: ""},
		ActorEmail:   "",
	}, testSiteURL())

	require.Equal(t, map[string]any{
		"activity":        "user_signed_up",
		"organization_id": "org_placeholder",
		"signup_source":   "invited",
		"dashboard_url":   "https://app.example.test",
	}, built.Properties)
}

// testSiteURL is the dashboard base URL the property builder falls back to when
// an activity carries no subject page of its own.
func testSiteURL() *url.URL {
	return &url.URL{Scheme: "https", Host: "app.example.test"}
}

// dashboard_url is the one property that is never omitted. A Slack destination
// that renders it as a button link fails the entire message when the url is
// empty, so an activity with no subject page of its own must still resolve to
// something valid.
func TestBuildEventAlwaysSetsDashboardURL(t *testing.T) {
	t.Parallel()

	subject := growthsignals.BuildEvent(growthsignals.ActivityEvent{
		Activity:       growthsignals.ActivityProjectCreated,
		OrganizationID: "org_placeholder",
		DashboardURL:   "https://app.example.test/acme/widgets",
	}, growthsignals.Enrichment{
		Organization: growthsignals.OrganizationDetails{Slug: "acme"},
	}, testSiteURL())
	require.Equal(t, "https://app.example.test/acme/widgets", subject.Properties["dashboard_url"])

	organization := growthsignals.BuildEvent(growthsignals.ActivityEvent{
		Activity:       growthsignals.ActivityProjectCreated,
		OrganizationID: "org_placeholder",
	}, growthsignals.Enrichment{
		Organization: growthsignals.OrganizationDetails{Slug: "acme"},
	}, testSiteURL())
	require.Equal(t, "https://app.example.test/acme", organization.Properties["dashboard_url"])

	root := growthsignals.BuildEvent(growthsignals.ActivityEvent{
		Activity:       growthsignals.ActivityProjectCreated,
		OrganizationID: "org_placeholder",
	}, growthsignals.Enrichment{}, testSiteURL())
	require.Equal(t, "https://app.example.test", root.Properties["dashboard_url"])
}

// An extra may never occupy a key the event shape owns. Writing extras first is
// not enough on its own, because a base property whose value is empty is
// omitted rather than written, and the project keys are skipped entirely on an
// organization-scoped activity.
func TestBuildEventExtrasCannotOccupyReservedKeys(t *testing.T) {
	t.Parallel()

	built := growthsignals.BuildEvent(growthsignals.ActivityEvent{
		Activity:       growthsignals.ActivityUserSignedUp,
		OrganizationID: "org_placeholder",
		Extra: map[string]string{
			"organization_slug":                "attacker-owned",
			"project_slug":                     "claimed",
			"activity":                         "something_else",
			growthsignals.PropertySignupSource: growthsignals.SignupSourceOrganic,
		},
	}, growthsignals.Enrichment{}, testSiteURL())

	require.Equal(t, "user_signed_up", built.Properties["activity"])
	require.NotContains(t, built.Properties, "organization_slug")
	require.NotContains(t, built.Properties, "project_slug")
	require.Equal(t, growthsignals.SignupSourceOrganic, built.Properties[growthsignals.PropertySignupSource])
}

// With no site URL configured there is no link to report. Reporting an empty
// string would be worse than reporting nothing, because a Slack destination
// that renders it as a button link fails the whole message on a blank url.
func TestBuildEventOmitsDashboardURLWithoutSiteURL(t *testing.T) {
	t.Parallel()

	built := growthsignals.BuildEvent(growthsignals.ActivityEvent{
		Activity:       growthsignals.ActivityProjectCreated,
		OrganizationID: "org_placeholder",
	}, growthsignals.Enrichment{}, nil)

	require.NotContains(t, built.Properties, "dashboard_url")
}
