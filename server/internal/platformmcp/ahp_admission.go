package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/management/readmodel"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/oauth"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// LiveAHPProjects admits human members with a live project:read grant.
// Organization administration privileges are not required.
type LiveAHPProjects struct {
	authorizer *LiveOrgAdminAuthorizer
	reader     *readmodel.Reader
	engine     *authz.Engine
}

func NewAHPProjects(db *pgxpool.Pool, engine *authz.Engine) *LiveAHPProjects {
	return &LiveAHPProjects{authorizer: NewLiveOrgAdminAuthorizer(db, engine), reader: readmodel.New(db), engine: engine}
}

func (a *LiveAHPProjects) EligibleProjects(ctx context.Context, principal Principal) ([]AHPProjectOption, error) {
	if !ahpOriginAllowsOrganization(ctx, principal.OrganizationID) {
		return nil, ErrForbidden
	}
	ctx, err := a.authorizer.PrepareExternalContext(ctx, principal)
	if err != nil {
		return nil, ahpAdmissionError(err)
	}
	result := []AHPProjectOption{}
	after := uuid.Nil
	for {
		rows, err := a.reader.ListProjectsPage(ctx, principal.OrganizationID, after, 100)
		if err != nil {
			return nil, fmt.Errorf("%w: list AHP projects: %w", ErrUnavailable, err)
		}
		if len(rows) == 0 {
			return result, nil
		}
		checks := make([]authz.Check, len(rows))
		for i, row := range rows {
			checks[i] = authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: row.ID.String(), Dimensions: nil}
		}
		allowed, err := a.engine.FindMatched(ctx, checks)
		if err != nil {
			return nil, ahpAdmissionError(err)
		}
		for i, row := range rows {
			if allowed[i] {
				result = append(result, AHPProjectOption{ID: row.ID.String(), Name: row.Name, Slug: row.Slug})
			}
		}
		if len(rows) < 100 {
			return result, nil
		}
		after = rows[len(rows)-1].ID
	}
}

func (a *LiveAHPProjects) AuthorizeProject(ctx context.Context, principal Principal, projectID string) (context.Context, error) {
	if !ahpOriginAllowsOrganization(ctx, principal.OrganizationID) {
		return ctx, ErrForbidden
	}
	ctx, err := a.authorizer.PrepareExternalContext(ctx, principal)
	if err != nil {
		return ctx, ahpAdmissionError(err)
	}
	id, err := uuid.Parse(projectID)
	if err != nil {
		return ctx, ErrForbidden
	}
	project, err := a.reader.GetProject(ctx, id, principal.OrganizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ctx, ErrForbidden
	}
	if err != nil {
		return ctx, fmt.Errorf("%w: load AHP project: %w", ErrUnavailable, err)
	}
	if err := a.engine.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: id.String(), Dimensions: nil}); err != nil {
		return ctx, ahpAdmissionError(err)
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return ctx, ErrUnavailable
	}
	bound := *authCtx
	bound.ProjectID, bound.ProjectSlug = &project.ID, &project.Slug
	return contextvalues.WithAuthenticatedActor(ctx, &bound, urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID)), nil
}

type AHPSessionStore interface {
	GetActiveSession(ctx context.Context, organizationID, jti string) (oauth.Session, error)
}

type AHPAuthenticator struct {
	signer      *sessiontokens.Signer
	store       AHPSessionStore
	credentials *CredentialCodec
	baseURL     *url.URL
	projects    AHPProjects
}

func NewAHPAuthenticator(signer *sessiontokens.Signer, store AHPSessionStore, encryptionClient *encryption.Client, baseURL *url.URL, projects AHPProjects) (*AHPAuthenticator, error) {
	if signer == nil || store == nil || baseURL == nil || baseURL.Scheme == "" || baseURL.Host == "" || projects == nil {
		return nil, errors.New("AHP authenticator requires signer, store, base URL and projects")
	}
	credentials, err := NewCredentialCodec(encryptionClient)
	if err != nil {
		return nil, ahpAdmissionError(err)
	}
	credentials.namespace = "ahp"
	return &AHPAuthenticator{signer: signer, store: store, credentials: credentials, baseURL: baseURL, projects: projects}, nil
}

func (a *AHPAuthenticator) AuthenticateAHP(ctx context.Context, token, projectSlug string) (context.Context, error) {
	base, err := url.Parse(requestorigin.BaseURL(ctx, a.baseURL.String()))
	if err != nil {
		return ctx, ErrUnavailable
	}
	resource := ahpResource(base)
	claims, err := a.signer.ValidateExactAudience(token, resource)
	if err != nil || claims.Issuer != resource {
		return ctx, ErrUnauthorized
	}
	subject, err := urn.ParseSessionSubject(claims.Subject)
	if err != nil || subject.Kind != urn.SessionSubjectKindUser {
		return ctx, ErrUnauthorized
	}
	orgID, err := a.credentials.OrganizationID(accessJTICredential, claims.ID)
	if err != nil {
		return ctx, ErrUnauthorized
	}
	if !ahpOriginAllowsOrganization(ctx, orgID) {
		return ctx, ErrForbidden
	}
	session, err := a.store.GetActiveSession(ctx, orgID, claims.ID)
	if err != nil {
		if errors.Is(err, oauth.ErrNotFound) || errors.Is(err, oauth.ErrRevoked) || errors.Is(err, oauth.ErrExpired) || errors.Is(err, pgx.ErrNoRows) {
			return ctx, ErrUnauthorized
		}
		return ctx, fmt.Errorf("%w: load AHP session: %w", ErrUnavailable, err)
	}
	connection := session.Connection
	if session.ClientID != claims.ClientID || connection.Subject != subject.String() || connection.ResourceURI != resource || connection.ProjectID == "" || connection.OrganizationID != orgID {
		return ctx, ErrUnauthorized
	}
	principal := Principal{UserID: subject.ID, OrganizationID: orgID, ClientID: session.ClientID, ConnectionID: connection.ID, Generation: connection.Generation, Surface: SurfacePlatformMCP}
	ctx, err = a.projects.AuthorizeProject(ctx, principal, connection.ProjectID)
	if err != nil {
		return ctx, fmt.Errorf("authorize AHP session project: %w", err)
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectSlug == nil {
		return ctx, ErrUnavailable
	}
	if projectSlug != "" && projectSlug != *authCtx.ProjectSlug {
		return ctx, ErrForbidden
	}
	return ctx, nil
}

// Distinguish revoked authorization from an unavailable authorization backend.
func ahpAdmissionError(err error) error {
	if isAuthorizationDenied(err) {
		return fmt.Errorf("%w: %w", ErrForbidden, err)
	}
	return fmt.Errorf("%w: %w", ErrUnavailable, err)
}

// The routing middleware binds custom-domain requests to their owning tenant.
// A valid resource audience does not authorize another tenant on that host.
func ahpOriginAllowsOrganization(ctx context.Context, organizationID string) bool {
	origin, ok := requestorigin.FromContext(ctx)
	return !ok || origin.OrganizationID == "" || origin.OrganizationID == organizationID
}
