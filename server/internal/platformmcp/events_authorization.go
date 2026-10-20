package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	eventrepo "github.com/speakeasy-api/gram/server/internal/mcpevents/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/feature"
	approvalrepo "github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpevents"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// EventAuthorizer loads live grants for both HTTP subscriptions and Pub/Sub delivery.
// It never depends on a request context's cached grants or an OAuth access token.
type EventAuthorizer struct {
	db    eventrepo.DBTX
	flags feature.Provider
	now   func() time.Time
}

// NewEventAuthorizer uses the same durable capability and review rollout as tools.
func NewEventAuthorizer(db *pgxpool.Pool, flags feature.Provider) *EventAuthorizer {
	return &EventAuthorizer{db: db, flags: flags, now: time.Now}
}

// WithTx prevents nested pool acquisition while subscriptions or deliveries hold locks.
func (a *EventAuthorizer) WithTx(tx pgx.Tx) mcpevents.Authorizer {
	transactional := *a
	transactional.db = tx
	return &transactional
}

// Authorize accepts a nil project only for event-type discovery. A request ID
// additionally proves the review is still visible to the exact recipient.
func (a *EventAuthorizer) Authorize(ctx context.Context, owner mcpevents.Owner, projectID uuid.UUID, eventName string, requestID uuid.UUID) error {
	if owner.UserID == "" || owner.OrganizationID == "" || owner.ConnectionID == uuid.Nil || owner.Generation == uuid.Nil || owner.ClientID == "" {
		return mcpevents.ErrDenied
	}
	if eventName != mcpevents.ReviewRequested && eventName != mcpevents.ReviewDecided {
		return mcpevents.ErrDenied
	}
	enabled, err := productfeatures.IsFeatureEnabledUncached(ctx, a.db, owner.OrganizationID, productfeatures.FeaturePlatformMCP)
	if err != nil {
		return fmt.Errorf("check event capability: %w", err)
	}
	if !enabled {
		return mcpevents.ErrDenied
	}
	connection, err := platformrepo.New(a.db).GetActivePlatformMCPConnectionByID(ctx, platformrepo.GetActivePlatformMCPConnectionByIDParams{ID: owner.ConnectionID, OrganizationID: owner.OrganizationID})
	if errors.Is(err, pgx.ErrNoRows) {
		return mcpevents.ErrDenied
	}
	if err != nil {
		return fmt.Errorf("read event connection: %w", err)
	}
	expiresAt := connectionAuthorizationExpiry(connection.AuthorizationExpiresAt, connection.AuthorizedAt, connection.ReauthorizedAt)
	if connection.ActiveGeneration != owner.Generation || connection.ClientID != owner.ClientID || connection.SubjectUrn != urn.NewPrincipal(urn.PrincipalTypeUser, owner.UserID).String() || connection.ReauthorizationRequiredAt.Valid || !expiresAt.After(a.now()) {
		return mcpevents.ErrDenied
	}
	organizations := orgrepo.New(a.db)
	member, err := organizations.HasActiveOrganizationUser(ctx, orgrepo.HasActiveOrganizationUserParams{OrganizationID: owner.OrganizationID, UserID: owner.UserID})
	if err != nil {
		return fmt.Errorf("check event membership: %w", err)
	}
	if !member {
		return mcpevents.ErrDenied
	}
	org, err := organizations.GetOrganizationMetadata(ctx, owner.OrganizationID)
	if err != nil {
		return fmt.Errorf("read event organization: %w", err)
	}
	for _, flag := range []feature.Flag{feature.FlagMCPApproval, feature.FlagPlatformMCPEvents} {
		enabled, err := a.flags.IsFlagEnabled(ctx, flag, owner.OrganizationID, feature.OrgProjectGroups(org.Slug, ""))
		if err != nil {
			return fmt.Errorf("check event rollout: %w", err)
		}
		if !enabled {
			return mcpevents.ErrDenied
		}
	}
	principals, err := authz.ResolveUserPrincipals(ctx, a.db, owner.OrganizationID, owner.UserID)
	if err != nil {
		return fmt.Errorf("resolve event principals: %w", err)
	}
	grants, err := authz.LoadGrants(ctx, a.db, owner.OrganizationID, principals)
	if err != nil {
		return fmt.Errorf("load event grants: %w", err)
	}
	checks := []authz.Check{}
	if eventName == mcpevents.ReviewRequested {
		checks = append(checks, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: owner.OrganizationID, Dimensions: nil})
	}
	if projectID != uuid.Nil {
		_, err := projectsrepo.New(a.db).GetProjectByIDAndOrganizationID(ctx, projectsrepo.GetProjectByIDAndOrganizationIDParams{ID: projectID, OrganizationID: owner.OrganizationID})
		if errors.Is(err, pgx.ErrNoRows) {
			return mcpevents.ErrDenied
		}
		if err != nil {
			return fmt.Errorf("read event project: %w", err)
		}
		checks = append(checks, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: projectID.String(), Dimensions: nil})
	}
	for _, check := range checks {
		allowed, err := authz.GrantsAuthorize(grants, check)
		if err != nil {
			return fmt.Errorf("check event grants: %w", err)
		}
		if !allowed {
			return mcpevents.ErrDenied
		}
	}
	if requestID != uuid.Nil {
		if eventName == mcpevents.ReviewDecided {
			_, err = approvalrepo.New(a.db).GetPlatformRequesterApprovalRequest(ctx, approvalrepo.GetPlatformRequesterApprovalRequestParams{ID: requestID, ProjectID: projectID, OrganizationID: owner.OrganizationID, UserID: owner.UserID})
		} else {
			_, err = approvalrepo.New(a.db).GetApprovalRequest(ctx, approvalrepo.GetApprovalRequestParams{ID: requestID, ProjectID: projectID})
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return mcpevents.ErrDenied
		}
		if err != nil {
			return fmt.Errorf("read event review: %w", err)
		}
	}
	return nil
}

func eventOwner(principal Principal) (mcpevents.Owner, error) {
	id, idErr := uuid.Parse(principal.ConnectionID)
	generation, generationErr := uuid.Parse(principal.Generation)
	if idErr != nil || generationErr != nil {
		return mcpevents.Owner{}, ErrUnauthorized
	}
	return mcpevents.Owner{OrganizationID: principal.OrganizationID, UserID: principal.UserID, ConnectionID: id, Generation: generation, ClientID: principal.ClientID}, nil
}
