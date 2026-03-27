package access

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	srv "github.com/speakeasy-api/gram/server/gen/http/access/server"
	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var errWorkOSOrganizationNotLinked = errors.New("organization is not linked to WorkOS")

type Service struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
	auth   *auth.Auth
	roles  workos.RoleProvider
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, sessions *sessions.Manager, roles workos.RoleProvider) *Service {
	logger = logger.With(attr.SlogComponent("access"))

	return &Service{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/access"),
		logger: logger,
		db:     db,
		auth:   auth.New(logger, db, sessions),
		roles:  roles,
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) ListRoles(ctx context.Context, _ *gen.ListRolesPayload) (*gen.ListRolesResult, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "missing auth context").Log(ctx, s.logger)
	}
	if s.roles == nil {
		return nil, oops.E(oops.CodeGatewayError, nil, "role provider is not configured").Log(ctx, s.logger)
	}

	workosOrgID, err := s.workosOrgID(ctx, ac.ActiveOrganizationID)
	switch {
	case errors.Is(err, errWorkOSOrganizationNotLinked):
		return nil, oops.E(oops.CodeBadRequest, nil, "organization is not linked to WorkOS").Log(ctx, s.logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "get organization metadata").Log(ctx, s.logger)
	default:
	}

	wRoles, err := s.roles.ListRoles(ctx, workosOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "list roles from workos").Log(ctx, s.logger)
	}

	members, err := s.roles.ListMembers(ctx, workosOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "list members from workos").Log(ctx, s.logger)
	}
	memberCounts := make(map[string]int)
	for _, member := range members {
		memberCounts[member.RoleSlug]++
	}

	roles := make([]*gen.Role, 0, len(wRoles))
	for _, wr := range wRoles {
		grants, err := s.grantsForRole(ctx, ac.ActiveOrganizationID, wr.Slug)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "list grants for role").Log(ctx, s.logger)
		}
		roles = append(roles, &gen.Role{
			ID:          wr.ID,
			Name:        wr.Name,
			Description: wr.Description,
			IsSystem:    isSystemRole(wr.Slug),
			Grants:      grants,
			MemberCount: memberCounts[wr.Slug],
			CreatedAt:   time.Time{}.UTC().Format(time.RFC3339),
			UpdatedAt:   time.Time{}.UTC().Format(time.RFC3339),
		})
	}

	return &gen.ListRolesResult{Roles: roles}, nil
}

func (s *Service) GetRole(ctx context.Context, payload *gen.GetRolePayload) (*gen.Role, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "missing auth context").Log(ctx, s.logger)
	}
	if s.roles == nil {
		return nil, oops.E(oops.CodeGatewayError, nil, "role provider is not configured").Log(ctx, s.logger)
	}

	workosOrgID, err := s.workosOrgID(ctx, ac.ActiveOrganizationID)
	switch {
	case errors.Is(err, errWorkOSOrganizationNotLinked):
		return nil, oops.E(oops.CodeBadRequest, nil, "organization is not linked to WorkOS").Log(ctx, s.logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "get organization metadata").Log(ctx, s.logger)
	default:
	}

	wRoles, err := s.roles.ListRoles(ctx, workosOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "list roles from workos").Log(ctx, s.logger)
	}

	for _, wr := range wRoles {
		if wr.ID != payload.ID {
			continue
		}

		grants, err := s.grantsForRole(ctx, ac.ActiveOrganizationID, wr.Slug)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "list grants for role").Log(ctx, s.logger)
		}

		members, err := s.roles.ListMembers(ctx, workosOrgID)
		if err != nil {
			return nil, oops.E(oops.CodeGatewayError, err, "list members from workos").Log(ctx, s.logger)
		}

		memberCount := 0
		for _, member := range members {
			if member.RoleSlug == wr.Slug {
				memberCount++
			}
		}

		return &gen.Role{
			ID:          wr.ID,
			Name:        wr.Name,
			Description: wr.Description,
			IsSystem:    isSystemRole(wr.Slug),
			Grants:      grants,
			MemberCount: memberCount,
			CreatedAt:   time.Time{}.UTC().Format(time.RFC3339),
			UpdatedAt:   time.Time{}.UTC().Format(time.RFC3339),
		}, nil
	}

	return nil, oops.E(oops.CodeNotFound, nil, "role not found").Log(ctx, s.logger)
}

// CreateRole is creates a role for a user of a given organization.
// It is an idempotent operation intentionally ordered so that member assignment happens last.
// If WorkOS role creation succeeds but local grant sync fails, we return an
// error with no users assigned to the new role. That leaves a partially
// created role behind, but keeps the outcome safe and retryable: repeating the
// request can finish configuration without having granted accidental access.
func (s *Service) CreateRole(ctx context.Context, payload *gen.CreateRolePayload) (*gen.Role, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "missing auth context").Log(ctx, s.logger)
	}
	if s.roles == nil {
		return nil, oops.E(oops.CodeGatewayError, nil, "role provider is not configured").Log(ctx, s.logger)
	}

	workosOrgID, err := s.workosOrgID(ctx, ac.ActiveOrganizationID)
	switch {
	case errors.Is(err, errWorkOSOrganizationNotLinked):
		return nil, oops.E(oops.CodeBadRequest, nil, "organization is not linked to WorkOS").Log(ctx, s.logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "get organization metadata").Log(ctx, s.logger)
	default:
	}

	roleSlug := conv.ToSlug(payload.Name)
	wr, err := s.roles.CreateRole(ctx, workosOrgID, workos.CreateRoleOpts{
		Name:        payload.Name,
		Slug:        roleSlug,
		Description: payload.Description,
	})
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "create role in workos").Log(ctx, s.logger)
	}

	// Stop before assigning members if grant sync fails. That can leave behind a
	// newly created WorkOS role with no local grants, but it avoids assigning users
	// to a role whose effective permissions are incomplete or unknown. Returning an
	// error makes the setup retryable without creating accidental access.
	if err := s.syncGrants(ctx, ac.ActiveOrganizationID, wr.Slug, roleGrantPayloads(payload.Grants)); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "sync grants for created role").Log(ctx, s.logger)
	}

	assignedCount := 0
	if len(payload.MemberIds) > 0 {
		members, err := s.roles.ListMembers(ctx, workosOrgID)
		if err != nil {
			return nil, oops.E(oops.CodeGatewayError, err, "list members from workos").Log(ctx, s.logger)
		}

		membershipByUser := make(map[string]string, len(members))
		for _, member := range members {
			membershipByUser[member.UserID] = member.ID
		}

		for _, userID := range payload.MemberIds {
			membershipID, ok := membershipByUser[userID]
			if !ok {
				continue
			}

			if _, err := s.roles.UpdateMemberRole(ctx, membershipID, wr.Slug); err != nil {
				return nil, oops.E(oops.CodeGatewayError, err, "assign members to created role").Log(ctx, s.logger)
			}

			assignedCount++
		}
	}

	grants, err := s.grantsForRole(ctx, ac.ActiveOrganizationID, wr.Slug)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list grants for role").Log(ctx, s.logger)
	}

	return &gen.Role{
		ID:          wr.ID,
		Name:        wr.Name,
		Description: wr.Description,
		IsSystem:    false,
		Grants:      grants,
		MemberCount: assignedCount,
		CreatedAt:   time.Time{}.UTC().Format(time.RFC3339),
		UpdatedAt:   time.Time{}.UTC().Format(time.RFC3339),
	}, nil
}

func (s *Service) UpdateRole(ctx context.Context, payload *gen.UpdateRolePayload) (*gen.Role, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "missing auth context").Log(ctx, s.logger)
	}
	if s.roles == nil {
		return nil, oops.E(oops.CodeGatewayError, nil, "role provider is not configured").Log(ctx, s.logger)
	}

	workosOrgID, err := s.workosOrgID(ctx, ac.ActiveOrganizationID)
	switch {
	case errors.Is(err, errWorkOSOrganizationNotLinked):
		return nil, oops.E(oops.CodeBadRequest, nil, "organization is not linked to WorkOS").Log(ctx, s.logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "get organization metadata").Log(ctx, s.logger)
	default:
	}

	wRoles, err := s.roles.ListRoles(ctx, workosOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "list roles from workos").Log(ctx, s.logger)
	}

	var currentRole *workos.Role
	for i := range wRoles {
		if wRoles[i].ID == payload.ID {
			currentRole = &wRoles[i]
			break
		}
	}
	if currentRole == nil {
		return nil, oops.E(oops.CodeNotFound, nil, "role not found").Log(ctx, s.logger)
	}
	if isSystemRole(currentRole.Slug) {
		return nil, oops.E(oops.CodeBadRequest, nil, "system roles cannot be updated").Log(ctx, s.logger)
	}

	updatedRole, err := s.roles.UpdateRole(ctx, workosOrgID, currentRole.Slug, workos.UpdateRoleOpts{
		Name:        payload.Name,
		Description: payload.Description,
	})
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "update role in workos").Log(ctx, s.logger)
	}

	// As with role creation, member reassignment happens after local grant sync so
	// a failed sync never leaves users attached to a role with incomplete access.
	if payload.Grants != nil {
		if err := s.syncGrants(ctx, ac.ActiveOrganizationID, currentRole.Slug, roleGrantPayloads(payload.Grants)); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "sync grants for updated role").Log(ctx, s.logger)
		}
	}

	if payload.MemberIds != nil {
		members, err := s.roles.ListMembers(ctx, workosOrgID)
		if err != nil {
			return nil, oops.E(oops.CodeGatewayError, err, "list members from workos").Log(ctx, s.logger)
		}

		membershipByUser := make(map[string]string, len(members))
		for _, member := range members {
			membershipByUser[member.UserID] = member.ID
		}

		for _, userID := range payload.MemberIds {
			membershipID, ok := membershipByUser[userID]
			if !ok {
				continue
			}

			if _, err := s.roles.UpdateMemberRole(ctx, membershipID, currentRole.Slug); err != nil {
				return nil, oops.E(oops.CodeGatewayError, err, "assign members to updated role").Log(ctx, s.logger)
			}
		}
	}

	grants, err := s.grantsForRole(ctx, ac.ActiveOrganizationID, currentRole.Slug)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list grants for role").Log(ctx, s.logger)
	}

	members, err := s.roles.ListMembers(ctx, workosOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "list members from workos").Log(ctx, s.logger)
	}
	memberCount := 0
	for _, member := range members {
		if member.RoleSlug == currentRole.Slug {
			memberCount++
		}
	}

	return &gen.Role{
		ID:          updatedRole.ID,
		Name:        updatedRole.Name,
		Description: updatedRole.Description,
		IsSystem:    false,
		Grants:      grants,
		MemberCount: memberCount,
		CreatedAt:   time.Time{}.UTC().Format(time.RFC3339),
		UpdatedAt:   time.Time{}.UTC().Format(time.RFC3339),
	}, nil
}

func (s *Service) DeleteRole(context.Context, *gen.DeleteRolePayload) error {
	return oops.E(oops.CodeNotImplemented, nil, "not implemented")
}

func (s *Service) ListMembers(context.Context, *gen.ListMembersPayload) (*gen.ListMembersResult, error) {
	return nil, oops.E(oops.CodeNotImplemented, nil, "not implemented")
}

func (s *Service) UpdateMemberRole(context.Context, *gen.UpdateMemberRolePayload) (*gen.AccessMember, error) {
	return nil, oops.E(oops.CodeNotImplemented, nil, "not implemented")
}

func (s *Service) ListGrants(ctx context.Context, payload *gen.ListGrantsPayload) (*gen.ListGrantsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	rows, err := repo.New(s.db).ListPrincipalGrantsByOrg(ctx, repo.ListPrincipalGrantsByOrgParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		PrincipalUrn:   conv.PtrValOr(payload.PrincipalUrn, ""),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list principal grants").Log(ctx, s.logger)
	}

	grants := make([]*gen.Grant, len(rows))
	for i, row := range rows {
		grants[i] = grantFromRow(row)
	}

	return &gen.ListGrantsResult{Grants: grants}, nil
}

func (s *Service) UpsertGrants(ctx context.Context, payload *gen.UpsertGrantsPayload) (*gen.UpsertGrantsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to access grants").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	tr := repo.New(dbtx)

	grants := make([]*gen.Grant, 0, len(payload.Grants))

	for _, form := range payload.Grants {
		if form == nil {
			continue
		}

		row, err := tr.UpsertPrincipalGrant(ctx, repo.UpsertPrincipalGrantParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			PrincipalUrn:   form.PrincipalUrn,
			Scope:          form.Scope,
			Resource:       form.Resource,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to add or update grant").Log(ctx, s.logger)
		}

		grants = append(grants, grantFromRow(row))
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to save updated grants").Log(ctx, s.logger)
	}

	return &gen.UpsertGrantsResult{Grants: grants}, nil
}

func (s *Service) RemoveGrants(ctx context.Context, payload *gen.RemoveGrantsPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to access grants").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	tr := repo.New(dbtx)

	for _, entry := range payload.Grants {
		if entry == nil {
			continue
		}

		_, err = tr.DeletePrincipalGrantByTuple(ctx, repo.DeletePrincipalGrantByTupleParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			PrincipalUrn:   entry.PrincipalUrn,
			Scope:          entry.Scope,
			Resource:       entry.Resource,
		})
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to remove grant").Log(ctx, s.logger)
		}
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to save grant removals").Log(ctx, s.logger)
	}

	return nil
}

func (s *Service) RemovePrincipalGrants(ctx context.Context, payload *gen.RemovePrincipalGrantsPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	_, err := repo.New(s.db).DeletePrincipalGrantsByPrincipal(ctx, repo.DeletePrincipalGrantsByPrincipalParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		PrincipalUrn:   payload.PrincipalUrn,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to remove principal grants").Log(ctx, s.logger)
	}

	return nil
}

type RoleGrant struct {
	Scope     string
	Resources []string
}

func (s *Service) syncGrants(ctx context.Context, orgID string, roleSlug string, grants []*RoleGrant) error {
	if orgID == "" {
		return fmt.Errorf("organization id is required")
	}

	principalURN := urn.NewPrincipal(urn.PrincipalTypeRole, roleSlug)

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin grant sync transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	q := repo.New(tx)

	if _, err := q.DeletePrincipalGrantsByPrincipal(ctx, repo.DeletePrincipalGrantsByPrincipalParams{
		OrganizationID: orgID,
		PrincipalUrn:   principalURN,
	}); err != nil {
		return fmt.Errorf("delete grants for role %q: %w", roleSlug, err)
	}

	for _, grant := range grants {
		if grant == nil {
			continue
		}

		if grant.Resources == nil {
			if _, err := q.UpsertPrincipalGrant(ctx, repo.UpsertPrincipalGrantParams{
				OrganizationID: orgID,
				PrincipalUrn:   principalURN,
				Scope:          grant.Scope,
				Resource:       WildcardResource,
			}); err != nil {
				return fmt.Errorf("upsert unrestricted grant %q for role %q: %w", grant.Scope, roleSlug, err)
			}

			continue
		}

		for _, resource := range grant.Resources {
			if _, err := q.UpsertPrincipalGrant(ctx, repo.UpsertPrincipalGrantParams{
				OrganizationID: orgID,
				PrincipalUrn:   principalURN,
				Scope:          grant.Scope,
				Resource:       resource,
			}); err != nil {
				return fmt.Errorf("upsert grant %q on resource %q for role %q: %w", grant.Scope, resource, roleSlug, err)
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit grant sync transaction: %w", err)
	}

	return nil
}

func (s *Service) grantsForRole(ctx context.Context, orgID string, roleSlug string) ([]*gen.RoleGrant, error) {
	rows, err := repo.New(s.db).ListPrincipalGrantsByOrg(ctx, repo.ListPrincipalGrantsByOrgParams{
		OrganizationID: orgID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeRole, roleSlug).String(),
	})
	if err != nil {
		return nil, fmt.Errorf("list grants for role %q: %w", roleSlug, err)
	}

	type scopeAgg struct {
		unrestricted bool
		resources    []string
	}
	byScope := make(map[string]*scopeAgg)
	for _, row := range rows {
		agg, ok := byScope[row.Scope]
		if !ok {
			agg = &scopeAgg{unrestricted: false, resources: nil}
			byScope[row.Scope] = agg
		}
		if row.Resource == WildcardResource {
			agg.unrestricted = true
			agg.resources = nil
			continue
		}
		if !agg.unrestricted {
			agg.resources = append(agg.resources, row.Resource)
		}
	}

	grants := make([]*gen.RoleGrant, 0, len(byScope))
	for scope, agg := range byScope {
		grant := &gen.RoleGrant{Scope: scope, Resources: nil}
		if agg.unrestricted {
			grant.Resources = nil
		} else {
			grant.Resources = append([]string(nil), agg.resources...)
		}
		grants = append(grants, grant)
	}

	return grants, nil
}

func (s *Service) authContext(ctx context.Context) (*contextvalues.AuthContext, error) {
	ac, ok := contextvalues.GetAuthContext(ctx)
	if !ok || ac == nil {
		return nil, errors.New("missing auth context")
	}

	return ac, nil
}

func (s *Service) workosOrgID(ctx context.Context, gramOrgID string) (string, error) {
	org, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, gramOrgID)
	if err != nil {
		return "", fmt.Errorf("get organization metadata: %w", err)
	}
	if !org.WorkosID.Valid || org.WorkosID.String == "" {
		return "", errWorkOSOrganizationNotLinked
	}

	return org.WorkosID.String, nil
}

func isSystemRole(roleSlug string) bool {
	switch roleSlug {
	case "admin", "member":
		return true
	default:
		return false
	}
}

func roleGrantPayloads(grants []*gen.RoleGrant) []*RoleGrant {
	out := make([]*RoleGrant, 0, len(grants))
	for _, grant := range grants {
		if grant == nil {
			continue
		}

		out = append(out, &RoleGrant{
			Scope:     grant.Scope,
			Resources: append([]string(nil), grant.Resources...),
		})
	}

	return out
}

func grantFromRow(row repo.PrincipalGrant) *gen.Grant {
	return &gen.Grant{
		ID:             row.ID.String(),
		OrganizationID: row.OrganizationID,
		PrincipalUrn:   row.PrincipalUrn.String(),
		PrincipalType:  row.PrincipalType,
		Scope:          row.Scope,
		Resource:       row.Resource,
		CreatedAt:      row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:      row.UpdatedAt.Time.Format(time.RFC3339),
	}
}
