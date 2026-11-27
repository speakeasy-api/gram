package orggateway

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/feature"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orghost"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// ProjectGatewayMember is one member server that the Project Gateway admits
// for a turn credential, together with the environment slug the gateway will
// use when serving it.
type ProjectGatewayMember struct {
	ServerID            uuid.UUID
	PlatformToolsetSlug string
	EnvironmentSlug     string
}

// ProjectGatewayEvaluation is the result of running Project Gateway
// availability, machine-access, credential admission, and member resolution
// for a single assistant turn credential.
type ProjectGatewayEvaluation struct {
	Admitted   bool
	GatewayURL string
	Reason     string
	Members    []ProjectGatewayMember
}

// ProjectGatewayAvailability describes whether a project's Project Gateway is
// served and eligible for machine access, together with the resolved HTTPS
// gateway URL and organization user-session issuer.
type ProjectGatewayAvailability struct {
	Available    bool
	Reason       string
	GatewayURL   *url.URL
	IssuerID     uuid.UUID
	Organization orgrepo.OrganizationMetadatum
	Project      projectsrepo.Project
}

// ResolveProjectGatewayParams configures ResolveProjectGateway.
type ResolveProjectGatewayParams struct {
	OrganizationID          string
	ProjectID               uuid.UUID
	ServerURL               *url.URL
	OrgHosts                *orghost.Resolver
	Features                feature.Provider
	RequireSharedIssuerMode bool
}

// ResolveProjectGateway is the single shared Project Gateway availability,
// HTTPS URL, and machine-access predicate used by both the Default runtime's
// per-turn eligibility evaluator and the Kit runtime's eligibility and
// bootstrap paths.
func ResolveProjectGateway(
	ctx context.Context,
	db usersessionsrepo.DBTX,
	p ResolveProjectGatewayParams,
) (ProjectGatewayAvailability, error) {
	deny := func(reason string) ProjectGatewayAvailability {
		var out ProjectGatewayAvailability
		out.Reason = reason
		return out
	}
	if p.OrganizationID == "" || p.ProjectID == uuid.Nil {
		return deny("missing_inputs"), nil
	}
	if p.Features == nil {
		return deny("machine_access_disabled"), nil
	}
	machineAccess, err := p.Features.IsFlagEnabledLocal(ctx, feature.FlagProjectGatewayMachineAccess, p.OrganizationID, nil, nil)
	if err != nil {
		return deny(""), fmt.Errorf("evaluate project gateway machine access: %w", err)
	}
	if !machineAccess {
		return deny("machine_access_disabled"), nil
	}

	organization, err := orgrepo.New(db).GetOrganizationMetadata(ctx, p.OrganizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return deny("organization_not_found"), nil
	}
	if err != nil {
		return deny(""), fmt.Errorf("load organization for project gateway: %w", err)
	}

	project, err := projectsrepo.New(db).GetProjectByIDAndOrganizationID(ctx, projectsrepo.GetProjectByIDAndOrganizationIDParams{
		ID:             p.ProjectID,
		OrganizationID: organization.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return deny("project_not_found"), nil
	}
	if err != nil {
		return deny(""), fmt.Errorf("load project for project gateway: %w", err)
	}

	baseURL := p.ServerURL
	if p.OrgHosts != nil {
		baseURL = p.OrgHosts.ServerURL(organization.DefaultHost)
	}
	if baseURL == nil {
		return deny("non_https_gateway_url"), nil
	}
	parsedURL, err := url.Parse(ProjectURL(baseURL, organization.Slug, project.Slug))
	if err != nil || parsedURL.Scheme != "https" || parsedURL.Host == "" || parsedURL.User != nil || parsedURL.RawQuery != "" || parsedURL.Fragment != "" {
		return deny("non_https_gateway_url"), nil
	}

	issuerID, unavailable, err := Availability(ctx, db, organization)
	if err != nil {
		return deny(""), fmt.Errorf("resolve project gateway availability: %w", err)
	}
	if unavailable != UnavailabilityNone {
		return deny(string(unavailable)), nil
	}

	if p.RequireSharedIssuerMode {
		issuer, err := usersessionsrepo.New(db).GetSharedUserSessionIssuerByID(ctx, issuerID)
		if errors.Is(err, pgx.ErrNoRows) {
			return deny("issuer_retired"), nil
		}
		if err != nil {
			return deny(""), fmt.Errorf("load project gateway issuer: %w", err)
		}
		if !authserver.IssuerInSharedMode(issuer.UserSessionIssuer) {
			return deny("issuer_not_shared_mode"), nil
		}
	}

	return ProjectGatewayAvailability{
		Available:    true,
		Reason:       "",
		GatewayURL:   parsedURL,
		IssuerID:     issuerID,
		Organization: organization,
		Project:      project,
	}, nil
}
