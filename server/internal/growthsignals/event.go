package growthsignals

import (
	"net/url"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Property keys every event may carry. They are the stable contract PostHog
// destinations filter and template against, so they are written once here
// rather than at each emission.
const (
	propertyActivity         = "activity"
	propertyOrganizationID   = "organization_id"
	propertyOrganizationSlug = "organization_slug"
	propertyOrganizationName = "organization_name"
	propertyProjectID        = "project_id"
	propertyProjectSlug      = "project_slug"
	propertyProjectName      = "project_name"
	propertyActorEmail       = "actor_email"
	propertyActorName        = "actor_name"
	propertySubjectName      = "subject_name"
	propertyActingSurface    = "acting_surface"
	propertyDashboardURL     = "dashboard_url"
	propertyAuditAction      = "audit_action"
)

// PropertyMcpKind is the flavour of MCP server an ActivityMcpServerCreated
// event describes.
const PropertyMcpKind = "mcp_kind"

// PropertySignupSource distinguishes an invited arrival from an organic one on
// an ActivityUserSignedUp event.
const PropertySignupSource = "signup_source"

// PropertyRole is the organization role granted on an
// ActivityMemberJoinedOrganization event.
const PropertyRole = "role"

// PropertyPolicyName is the name of the risk policy a security policy event
// describes.
const PropertyPolicyName = "policy_name"

const (
	// SignupSourceInvited marks a signup that had a pending invitation waiting
	// for its email address.
	SignupSourceInvited = "invited"

	// SignupSourceOrganic marks a signup that arrived without an invitation.
	SignupSourceOrganic = "organic"
)

// ActivityEvent is one notable moment, described in the terms its source
// already has. Ids here are resolved to names by an Enricher before the event
// is built, so producers never have to query for display values themselves.
//
// Every field beyond Activity and OrganizationID is optional: a producer sets
// what it knows and the resulting event carries what was set.
type ActivityEvent struct {
	// Activity is what happened. An empty activity or ActivitySkip is not
	// emitted.
	Activity Activity

	// OrganizationID is the Gram organization the activity belongs to. It is
	// the distinct id when no actor email resolves.
	OrganizationID string

	// ProjectID is the project the activity belongs to, or uuid.Nil for
	// organization-scoped activities.
	ProjectID uuid.UUID

	// ActorID identifies the acting principal, and is interpreted according to
	// ActorType: a Gram user id for a user, an email address for an email
	// principal, a role name for a role.
	ActorID string

	// ActorType is the kind of principal that acted. Only a user principal is
	// worth a user lookup, and only user and email principals have a person
	// behind them.
	ActorType urn.PrincipalType

	// ActorEmail is the acting user's email when the producer already knows it.
	// Setting it skips the lookup that would otherwise resolve ActorID.
	ActorEmail string

	// ActorName is the acting principal's display name.
	ActorName string

	// SubjectName is the display name of the thing acted on — the project that
	// was created, the MCP server that was updated.
	SubjectName string

	// ActingSurface is how the change was made: a dashboard session, an API
	// key, Platform MCP, an assistant.
	ActingSurface string

	// AuditAction is the audit action the activity was derived from, and is
	// empty for activities that are emitted directly rather than from the
	// audit log. It is reported so a surprising activity can be traced back to
	// the record that produced it.
	AuditAction audit.Action

	// DashboardURL deep links to the subject in the Gram dashboard. Leaving it
	// empty is allowed: BuildEvent falls back to the organization's page and
	// then to the site root, because the property must never be absent.
	DashboardURL string

	// Extra holds per-activity properties such as mcp_kind, signup_source,
	// role and policy_name. Blank values and the base property keys are
	// ignored, so an extra can never rewrite the event's identity.
	Extra map[string]string
}

// Enrichment is what an ActivityEvent's ids resolved to. Any part of it may be
// zero: a lookup that failed or found nothing narrows the event rather than
// dropping it.
type Enrichment struct {
	// Organization is what OrganizationID resolved to.
	Organization OrganizationDetails

	// Project is what ProjectID resolved to.
	Project ProjectDetails

	// ActorEmail is the acting user's email address.
	ActorEmail string
}

// CapturedEvent is the PostHog capture one activity produces.
type CapturedEvent struct {
	// Name is always EventName.
	Name string

	// DistinctID is the PostHog person the event attaches to.
	DistinctID string

	// Properties is the event's property map.
	Properties map[string]any
}

// BuildEvent assembles the PostHog capture for one activity.
//
// The distinct id is the actor's email whenever one resolved, so an activity
// lands on the same PostHog person as that user's signup and onboarding events.
// It falls back to the organization id, because role and system actors have no
// person behind them and an organization is still a useful thing to group by.
//
// reservedProperties are the keys the event shape owns. Per-activity extras may
// not write them, so an activity can never rename an organization or claim a
// project it does not belong to.
var reservedProperties = map[string]struct{}{
	propertyActivity:         {},
	propertyOrganizationID:   {},
	propertyOrganizationSlug: {},
	propertyOrganizationName: {},
	propertyProjectID:        {},
	propertyProjectSlug:      {},
	propertyProjectName:      {},
	propertyActorEmail:       {},
	propertyActorName:        {},
	propertySubjectName:      {},
	propertyActingSurface:    {},
	propertyDashboardURL:     {},
	propertyAuditAction:      {},
}

// Empty properties are omitted rather than sent blank. A blank organization
// name in Slack reads as an organization with no name; an absent one reads as
// what it is.
func BuildEvent(event ActivityEvent, enrichment Enrichment, siteURL *url.URL) CapturedEvent {
	properties := make(map[string]any, len(event.Extra)+15)

	// Extras go in first, but never on a key the event shape owns. Writing them
	// first is not enough on its own: a base property whose value is empty is
	// omitted rather than written, and the project keys are skipped entirely on
	// an organization-scoped activity, so without this filter an extra could
	// occupy a reserved key and change what the event appears to say.
	for key, value := range event.Extra {
		if key == "" || value == "" {
			continue
		}
		if _, reserved := reservedProperties[key]; reserved {
			continue
		}
		properties[key] = value
	}

	properties[propertyActivity] = string(event.Activity)

	setProperty(properties, propertyOrganizationID, event.OrganizationID)
	setProperty(properties, propertyOrganizationSlug, enrichment.Organization.Slug)
	setProperty(properties, propertyOrganizationName, enrichment.Organization.Name)

	if event.ProjectID != uuid.Nil {
		properties[propertyProjectID] = event.ProjectID.String()
		setProperty(properties, propertyProjectSlug, enrichment.Project.Slug)
		setProperty(properties, propertyProjectName, enrichment.Project.Name)
	}

	setProperty(properties, propertyActorEmail, enrichment.ActorEmail)
	setProperty(properties, propertyActorName, event.ActorName)
	setProperty(properties, propertySubjectName, event.SubjectName)
	setProperty(properties, propertyActingSurface, event.ActingSurface)
	setProperty(properties, propertyDashboardURL, dashboardURL(event, enrichment, siteURL))
	setProperty(properties, propertyAuditAction, string(event.AuditAction))

	return CapturedEvent{
		Name:       EventName,
		DistinctID: conv.Default(enrichment.ActorEmail, event.OrganizationID),
		Properties: properties,
	}
}

func setProperty(properties map[string]any, key string, value string) {
	if value == "" {
		return
	}

	properties[key] = value
}

// dashboardURL is the one property that is never omitted. Slack rejects a
// button whose url is empty and fails the whole message, so a destination
// template that links the subject would break every notification that happens
// to lack a link. An activity with no subject page falls back to the
// organization's page, and one whose organization did not resolve falls back to
// the site root, which is always a valid URL.
func dashboardURL(event ActivityEvent, enrichment Enrichment, siteURL *url.URL) string {
	if event.DashboardURL != "" {
		return event.DashboardURL
	}

	// No site URL configured. Reporting an empty string would be worse than
	// reporting nothing: a Slack destination that renders this as a button link
	// fails the whole message on a blank url, while an absent property lets the
	// template omit the button. The emitter warns about this at construction.
	if siteURL == nil {
		return ""
	}

	if enrichment.Organization.Slug != "" {
		return siteURL.JoinPath(enrichment.Organization.Slug).String()
	}

	return siteURL.String()
}
