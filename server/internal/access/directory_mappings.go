package access

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"go.opentelemetry.io/otel/trace"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/directory"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	directoryMappingKindGroup     = "group"
	directoryMappingKindAttribute = "attribute"
)

func (s *Service) ListDirectoryMappings(ctx context.Context, _ *gen.ListDirectoryMappingsPayload) (*gen.ListDirectoryMappingsResult, error) {
	ac, err := s.requireDirectoryMappingRead(ctx)
	if err != nil {
		return nil, err
	}

	organizationID := ac.ActiveOrganizationID
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(organizationID),
		attr.UserID(ac.UserID),
	)

	catalog, grantsByPrincipal, err := s.directoryMappingState(ctx, s.db, organizationID)
	if err != nil {
		return nil, err
	}

	mappings := make([]*gen.DirectoryMapping, 0, len(grantsByPrincipal))
	for _, principalURN := range sortedPrincipalURNs(grantsByPrincipal) {
		principal, err := parseDirectoryMappingPrincipal(principalURN)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "parse directory mapping principal").LogError(ctx, s.logger)
		}
		mappings = append(mappings, catalog.mapping(principal, scopedGrantsToGenRoleGrants(grantsByPrincipal[principalURN])))
	}

	return &gen.ListDirectoryMappingsResult{
		Mappings:   mappings,
		Groups:     catalog.groupTargets(),
		Attributes: catalog.attributeTargets(),
	}, nil
}

func (s *Service) UpsertDirectoryMapping(ctx context.Context, payload *gen.UpsertDirectoryMappingPayload) (*gen.DirectoryMapping, error) {
	ac, err := s.requireOrgAdmin(ctx)
	if err != nil {
		return nil, err
	}

	principal, err := parseDirectoryMappingPrincipal(payload.PrincipalUrn)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid directory mapping principal").LogError(ctx, s.logger)
	}
	if err := authz.ValidatePrincipal(ctx, s.db, ac.ActiveOrganizationID, principal); err != nil {
		switch {
		case errors.Is(err, authz.ErrPrincipalInvalid):
			return nil, oops.E(oops.CodeBadRequest, err, "invalid directory mapping principal").LogError(ctx, s.logger)
		case errors.Is(err, authz.ErrPrincipalNotFound):
			return nil, oops.E(oops.CodeNotFound, err, "directory mapping target not found").LogError(ctx, s.logger)
		default:
			return nil, oops.E(oops.CodeUnexpected, err, "validate directory mapping principal").LogError(ctx, s.logger)
		}
	}
	if len(payload.Grants) == 0 {
		return nil, oops.E(oops.CodeBadRequest, nil, "directory mappings require at least one grant").LogError(ctx, s.logger)
	}

	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
	)

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing resource").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	before, err := s.loadDirectoryMapping(ctx, dbtx, ac.ActiveOrganizationID, principal)
	if err != nil {
		return nil, err
	}

	if _, err := repo.New(dbtx).DeletePrincipalGrantsByPrincipal(ctx, repo.DeletePrincipalGrantsByPrincipalParams{
		OrganizationID: ac.ActiveOrganizationID,
		PrincipalUrn:   principal,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "replace directory mapping grants").LogError(ctx, s.logger)
	}
	if err := authz.PatchPrincipalGrants(ctx, dbtx, ac.ActiveOrganizationID, principal, roleGrantPayloads(payload.Grants), nil); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "write directory mapping grants").LogError(ctx, s.logger)
	}

	after, err := s.loadDirectoryMapping(ctx, dbtx, ac.ActiveOrganizationID, principal)
	if err != nil {
		return nil, err
	}

	if err := s.audit.LogAccessDirectoryMappingUpsert(ctx, dbtx, audit.LogAccessDirectoryMappingUpsertEvent{
		OrganizationID:        ac.ActiveOrganizationID,
		Actor:                 urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
		ActorDisplayName:      ac.Email,
		ActorSlug:             nil,
		PrincipalURN:          principal.String(),
		MappingSnapshotBefore: before,
		MappingSnapshotAfter:  after,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "write directory mapping audit log").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit directory mapping").LogError(ctx, s.logger)
	}

	return after, nil
}

func (s *Service) DeleteDirectoryMapping(ctx context.Context, payload *gen.DeleteDirectoryMappingPayload) error {
	ac, err := s.requireOrgAdmin(ctx)
	if err != nil {
		return err
	}

	principal, err := parseDirectoryMappingPrincipal(payload.PrincipalUrn)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid directory mapping principal").LogError(ctx, s.logger)
	}

	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
	)

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error accessing resource").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	before, err := s.loadDirectoryMapping(ctx, dbtx, ac.ActiveOrganizationID, principal)
	if err != nil {
		return err
	}
	if before == nil {
		return oops.E(oops.CodeNotFound, nil, "directory mapping not found").LogError(ctx, s.logger)
	}

	if _, err := repo.New(dbtx).DeletePrincipalGrantsByPrincipal(ctx, repo.DeletePrincipalGrantsByPrincipalParams{
		OrganizationID: ac.ActiveOrganizationID,
		PrincipalUrn:   principal,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete directory mapping grants").LogError(ctx, s.logger)
	}

	if err := s.audit.LogAccessDirectoryMappingDelete(ctx, dbtx, audit.LogAccessDirectoryMappingDeleteEvent{
		OrganizationID:        ac.ActiveOrganizationID,
		Actor:                 urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
		ActorDisplayName:      ac.Email,
		ActorSlug:             nil,
		PrincipalURN:          principal.String(),
		MappingSnapshotBefore: before,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "write directory mapping audit log").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit directory mapping delete").LogError(ctx, s.logger)
	}

	return nil
}

func (s *Service) requireDirectoryMappingRead(ctx context.Context) (*contextvalues.AuthContext, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "missing auth context").LogError(ctx, s.logger)
	}
	checks := []authz.Check{{
		Scope:        authz.ScopeOrgRead,
		ResourceKind: "",
		ResourceID:   ac.ActiveOrganizationID,
		Dimensions:   nil,
	}}
	if ac.ProjectID != nil {
		checks = append(checks, authz.Check{
			Scope:        authz.ScopeProjectRead,
			ResourceKind: "",
			ResourceID:   ac.ProjectID.String(),
			Dimensions:   nil,
		})
	}
	if err := s.authz.RequireAny(ctx, checks...); err != nil {
		return nil, err
	}
	return ac, nil
}

func (s *Service) loadDirectoryMapping(ctx context.Context, db repo.DBTX, organizationID string, principal urn.Principal) (*gen.DirectoryMapping, error) {
	catalog, grantsByPrincipal, err := s.directoryMappingState(ctx, db, organizationID)
	if err != nil {
		return nil, err
	}
	grants, ok := grantsByPrincipal[principal.String()]
	if !ok {
		return nil, nil
	}
	return catalog.mapping(principal, scopedGrantsToGenRoleGrants(grants)), nil
}

func (s *Service) directoryMappingState(ctx context.Context, db repo.DBTX, organizationID string) (directoryMappingCatalog, map[string][]*authz.ScopedGrant, error) {
	dir := directory.NewService(db)
	groups, err := dir.ListActiveGroups(ctx, organizationID)
	if err != nil {
		return directoryMappingCatalog{}, nil, oops.E(oops.CodeUnexpected, err, "list directory groups").LogError(ctx, s.logger)
	}
	attributes, err := dir.ListActiveAttributeValues(ctx, organizationID)
	if err != nil {
		return directoryMappingCatalog{}, nil, oops.E(oops.CodeUnexpected, err, "list directory attributes").LogError(ctx, s.logger)
	}

	rows, err := repo.New(db).ListPrincipalGrantsByTypes(ctx, repo.ListPrincipalGrantsByTypesParams{
		OrganizationID: organizationID,
		PrincipalTypes: []string{
			string(urn.PrincipalTypeDirectoryGroup),
			string(urn.PrincipalTypeDirectoryAttribute),
		},
	})
	if err != nil {
		return directoryMappingCatalog{}, nil, oops.E(oops.CodeUnexpected, err, "list directory mappings").LogError(ctx, s.logger)
	}

	grantsByPrincipal, err := directoryMappingGrantsByPrincipal(rows)
	if err != nil {
		return directoryMappingCatalog{}, nil, oops.E(oops.CodeUnexpected, err, "decode directory mapping grants").LogError(ctx, s.logger)
	}

	return newDirectoryMappingCatalog(groups, attributes), grantsByPrincipal, nil
}

func parseDirectoryMappingPrincipal(raw string) (urn.Principal, error) {
	principal, err := urn.ParsePrincipal(raw)
	if err != nil {
		return urn.Principal{}, fmt.Errorf("parse directory mapping principal: %w", err)
	}
	if principal.Type != urn.PrincipalTypeDirectoryGroup && principal.Type != urn.PrincipalTypeDirectoryAttribute {
		return urn.Principal{}, fmt.Errorf("%w: unsupported principal type %q", authz.ErrPrincipalInvalid, principal.Type)
	}
	return principal, nil
}

func directoryMappingGrantsByPrincipal(rows []repo.ListPrincipalGrantsByTypesRow) (map[string][]*authz.ScopedGrant, error) {
	grants := make([]authz.Grant, 0, len(rows))
	for _, row := range rows {
		selectors, err := authz.SelectorFromRow(row.Selectors)
		if err != nil {
			return nil, fmt.Errorf("unmarshal grant selector: %w", err)
		}
		grants = append(grants, authz.Grant{
			PrincipalUrn: row.PrincipalUrn.String(),
			Scope:        authz.Scope(row.Scope),
			Selector:     selectors,
		})
	}

	grouped := make(map[string][]authz.Grant, len(rows))
	for _, grant := range grants {
		grouped[grant.PrincipalUrn] = append(grouped[grant.PrincipalUrn], grant)
	}

	out := make(map[string][]*authz.ScopedGrant, len(grouped))
	for principalURN, principalGrants := range grouped {
		out[principalURN] = authz.GrantsToScopedGrants(principalGrants)
	}
	return out, nil
}

func scopedGrantsToGenRoleGrants(grants []*authz.ScopedGrant) []*gen.RoleGrant {
	out := make([]*gen.RoleGrant, 0, len(grants))
	for _, grant := range grants {
		out = append(out, scopedGrantToGenRoleGrant(grant))
	}
	return out
}

func sortedPrincipalURNs(grantsByPrincipal map[string][]*authz.ScopedGrant) []string {
	principalURNs := make([]string, 0, len(grantsByPrincipal))
	for principalURN := range grantsByPrincipal {
		principalURNs = append(principalURNs, principalURN)
	}
	slices.Sort(principalURNs)
	return principalURNs
}

type directoryMappingCatalog struct {
	groups     []directory.GroupSummary
	attributes []directory.AttributeValueSummary
	groupByID  map[string]directory.GroupSummary
	attrByURN  map[string]directory.AttributeValueSummary
}

func newDirectoryMappingCatalog(groups []directory.GroupSummary, attributes []directory.AttributeValueSummary) directoryMappingCatalog {
	catalog := directoryMappingCatalog{
		groups:     groups,
		attributes: attributes,
		groupByID:  make(map[string]directory.GroupSummary, len(groups)),
		attrByURN:  make(map[string]directory.AttributeValueSummary, len(attributes)),
	}
	for _, group := range groups {
		catalog.groupByID[group.ID.String()] = group
	}
	for _, attribute := range attributes {
		catalog.attrByURN[directory.AttributePrincipal(attribute.Key, attribute.Value)] = attribute
	}
	return catalog
}

func (c directoryMappingCatalog) groupTargets() []*gen.DirectoryGroupTarget {
	out := make([]*gen.DirectoryGroupTarget, 0, len(c.groups))
	for _, group := range c.groups {
		out = append(out, &gen.DirectoryGroupTarget{
			ID:           group.ID.String(),
			Name:         group.Name,
			PrincipalUrn: directory.GroupPrincipal(group.ID),
			MemberCount:  int(group.MemberCount),
		})
	}
	return out
}

func (c directoryMappingCatalog) attributeTargets() []*gen.DirectoryAttributeTarget {
	out := make([]*gen.DirectoryAttributeTarget, 0, len(c.attributes))
	for _, attribute := range c.attributes {
		out = append(out, &gen.DirectoryAttributeTarget{
			Key:          attribute.Key,
			Value:        attribute.Value,
			PrincipalUrn: directory.AttributePrincipal(attribute.Key, attribute.Value),
			MemberCount:  int(attribute.MemberCount),
		})
	}
	return out
}

func (c directoryMappingCatalog) mapping(principal urn.Principal, grants []*gen.RoleGrant) *gen.DirectoryMapping {
	mapping := &gen.DirectoryMapping{
		PrincipalUrn:   principal.String(),
		Kind:           "",
		GroupID:        nil,
		GroupName:      nil,
		AttributeKey:   nil,
		AttributeValue: nil,
		MemberCount:    0,
		Grants:         grants,
	}

	switch principal.Type {
	case urn.PrincipalTypeDirectoryGroup:
		mapping.Kind = directoryMappingKindGroup
		groupID := principal.ID
		mapping.GroupID = &groupID
		if group, ok := c.groupByID[groupID]; ok {
			name := group.Name
			mapping.GroupName = &name
			mapping.MemberCount = int(group.MemberCount)
		}
	case urn.PrincipalTypeDirectoryAttribute:
		mapping.Kind = directoryMappingKindAttribute
		if attribute, err := directory.ParseAttributePrincipal(principal.String()); err == nil {
			key := attribute.Key
			value := attribute.Value
			mapping.AttributeKey = &key
			mapping.AttributeValue = &value
			if summary, ok := c.attrByURN[principal.String()]; ok {
				mapping.MemberCount = int(summary.MemberCount)
			}
		}
	}

	return mapping
}
