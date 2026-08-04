package adminmcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	adminrepo "github.com/speakeasy-api/gram/server/internal/adminmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type JWTAuthenticator struct {
	signer   *sessiontokens.Signer
	store    *adminrepo.Queries
	issuer   string
	audience string
}

func NewJWTAuthenticator(signer *sessiontokens.Signer, db *pgxpool.Pool, issuer, audience string) *JWTAuthenticator {
	return &JWTAuthenticator{
		signer:   signer,
		store:    adminrepo.New(db),
		issuer:   issuer,
		audience: audience,
	}
}

func (a *JWTAuthenticator) Authenticate(ctx context.Context, token string) (Principal, error) {
	if a.signer == nil || a.store == nil || a.issuer == "" || a.audience == "" {
		return Principal{}, ErrUnavailable
	}

	claims, err := a.signer.Validate(token, a.audience)
	if err != nil || claims.Issuer != a.issuer {
		return Principal{}, ErrUnauthorized
	}
	subject, err := urn.ParseSessionSubject(claims.Subject)
	if err != nil || subject.Kind != urn.SessionSubjectKindUser {
		return Principal{}, ErrUnauthorized
	}

	session, err := a.store.GetActiveAdminMCPSessionByJTI(ctx, claims.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Principal{}, ErrUnauthorized
		}
		return Principal{}, fmt.Errorf("lookup active admin mcp session: %w", err)
	}
	if session.SubjectUrn != subject.String() || session.ClientID != claims.ClientID || session.ConnectionGeneration != session.ActiveGeneration {
		return Principal{}, ErrUnauthorized
	}

	return Principal{
		UserID:         subject.ID,
		OrganizationID: session.OrganizationID,
		ConnectionID:   session.ConnectionID.String(),
		Generation:     session.ConnectionGeneration.String(),
		ClientID:       session.ClientID,
	}, nil
}

type LiveOrgAdminAuthorizer struct {
	db     *pgxpool.Pool
	engine *authz.Engine
}

func NewLiveOrgAdminAuthorizer(db *pgxpool.Pool, engine *authz.Engine) *LiveOrgAdminAuthorizer {
	return &LiveOrgAdminAuthorizer{db: db, engine: engine}
}

// LiveOrganizationSelector returns only organizations where the current user
// holds the same live org:admin grant required to authorize Admin MCP.
type LiveOrganizationSelector struct {
	db         *pgxpool.Pool
	authorizer Authorizer
}

func NewLiveOrganizationSelector(db *pgxpool.Pool, authorizer Authorizer) *LiveOrganizationSelector {
	return &LiveOrganizationSelector{db: db, authorizer: authorizer}
}

func (s *LiveOrganizationSelector) EligibleOrganizations(ctx context.Context, userID string) ([]OrganizationOption, error) {
	if s == nil || s.db == nil || s.authorizer == nil || userID == "" {
		return nil, ErrUnavailable
	}
	organizations, err := organizationsrepo.New(s.db).ListOrganizationsForUser(ctx, pgtype.Text{String: userID, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("list organizations for admin selection: %w", err)
	}
	options := make([]OrganizationOption, 0, len(organizations))
	for _, organization := range organizations {
		if err := s.authorizer.RequireLiveOrgAdmin(ctx, Principal{UserID: userID, OrganizationID: organization.ID, ConnectionID: "", Generation: "", ClientID: ""}); err != nil {
			if isAuthorizationDenied(err) {
				continue
			}
			return nil, fmt.Errorf("check organization admin eligibility: %w", err)
		}
		options = append(options, OrganizationOption{ID: organization.ID, Name: organization.Name})
	}
	return options, nil
}

func isAuthorizationDenied(err error) bool {
	if errors.Is(err, ErrForbidden) {
		return true
	}
	var shareable *oops.ShareableError
	return errors.As(err, &shareable) && shareable.Code == oops.CodeForbidden
}

func (a *LiveOrgAdminAuthorizer) RequireLiveOrgAdmin(ctx context.Context, principal Principal) error {
	if a.db == nil || a.engine == nil || principal.UserID == "" || principal.OrganizationID == "" {
		return ErrUnavailable
	}

	member, err := organizationsrepo.New(a.db).HasActiveOrganizationUser(ctx, organizationsrepo.HasActiveOrganizationUserParams{
		UserID:         principal.UserID,
		OrganizationID: principal.OrganizationID,
	})
	if err != nil {
		return fmt.Errorf("check active organization membership: %w", err)
	}
	if !member {
		return ErrForbidden
	}

	principals, err := authz.ResolveUserPrincipals(ctx, a.db, principal.OrganizationID, principal.UserID)
	if err != nil {
		return fmt.Errorf("resolve live admin principals: %w", err)
	}
	grants, err := authz.LoadGrants(ctx, a.db, principal.OrganizationID, principals)
	if err != nil {
		return fmt.Errorf("load live admin grants: %w", err)
	}
	if err := a.engine.EvaluateLoadedGrants(ctx, grants, authz.Check{
		Scope:        authz.ScopeOrgAdmin,
		ResourceKind: "",
		ResourceID:   principal.OrganizationID,
		Dimensions:   nil,
	}); err != nil {
		return fmt.Errorf("require live org admin: %w", err)
	}
	return nil
}

type PostgresReader struct {
	store *adminrepo.Queries
}

func NewPostgresReader(db *pgxpool.Pool) *PostgresReader {
	return &PostgresReader{store: adminrepo.New(db)}
}

func (r *PostgresReader) GetAdminContext(_ context.Context, principal Principal) (AdminContext, error) {
	return AdminContext{
		OrganizationID: principal.OrganizationID,
		ConnectionID:   principal.ConnectionID,
		ReadOnly:       true,
	}, nil
}

func (r *PostgresReader) ListProjects(ctx context.Context, principal Principal, input ListProjectsInput) (ListProjectsOutput, error) {
	if r.store == nil {
		return ListProjectsOutput{}, ErrUnavailable
	}
	rows, err := r.store.ListAdminMCPProjects(ctx, adminrepo.ListAdminMCPProjectsParams{
		OrganizationID: principal.OrganizationID,
		LimitValue:     boundedLimitValue(input.Limit),
	})
	if err != nil {
		return ListProjectsOutput{}, fmt.Errorf("list admin mcp projects: %w", err)
	}

	output := ListProjectsOutput{Projects: make([]Project, 0, min(len(rows), input.Limit)), Truncated: false}
	for i, row := range rows {
		if i == input.Limit {
			output.Truncated = true
			break
		}
		output.Projects = append(output.Projects, Project{ID: row.ID.String(), Name: row.Name, Slug: row.Slug})
	}
	return output, nil
}

func (r *PostgresReader) ListProjectMCPs(ctx context.Context, principal Principal, input ListProjectMCPsInput) (ListProjectMCPsOutput, error) {
	if r.store == nil {
		return ListProjectMCPsOutput{}, ErrUnavailable
	}
	projectID, err := uuid.Parse(input.ProjectID)
	if err != nil {
		return ListProjectMCPsOutput{}, fmt.Errorf("parse project id: %w", err)
	}
	rows, err := r.store.ListAdminMCPServers(ctx, adminrepo.ListAdminMCPServersParams{
		ProjectID:      projectID,
		OrganizationID: principal.OrganizationID,
		LimitValue:     boundedLimitValue(input.Limit),
	})
	if err != nil {
		return ListProjectMCPsOutput{}, fmt.Errorf("list admin mcp servers: %w", err)
	}

	output := ListProjectMCPsOutput{MCPs: make([]MCP, 0, min(len(rows), input.Limit)), Truncated: false}
	for i, row := range rows {
		if i == input.Limit {
			output.Truncated = true
			break
		}
		output.MCPs = append(output.MCPs, mcpFromRow(row.ID, row.ProjectID, row.Name.String, row.Slug.String, row.Visibility))
	}
	return output, nil
}

func (r *PostgresReader) GetMCP(ctx context.Context, principal Principal, input GetMCPInput) (MCP, error) {
	if r.store == nil {
		return MCP{}, ErrUnavailable
	}
	projectID, err := uuid.Parse(input.ProjectID)
	if err != nil {
		return MCP{}, fmt.Errorf("parse project id: %w", err)
	}
	mcpID, err := uuid.Parse(input.MCPID)
	if err != nil {
		return MCP{}, fmt.Errorf("parse mcp id: %w", err)
	}
	row, err := r.store.GetAdminMCPServer(ctx, adminrepo.GetAdminMCPServerParams{
		McpServerID:    mcpID,
		ProjectID:      projectID,
		OrganizationID: principal.OrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return MCP{}, ErrForbidden
		}
		return MCP{}, fmt.Errorf("get admin mcp server: %w", err)
	}
	return mcpFromRow(row.ID, row.ProjectID, row.Name.String, row.Slug.String, row.Visibility), nil
}

func boundedLimitValue(limit int) int32 {
	return int32(boundedLimit(limit) + 1) // #nosec G115 -- boundedLimit caps the value at 100.
}

func mcpFromRow(id, projectID uuid.UUID, name, slug, visibility string) MCP {
	return MCP{
		ID:         id.String(),
		ProjectID:  projectID.String(),
		Name:       name,
		Slug:       slug,
		Visibility: visibility,
	}
}
