package access

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/trace"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	directoryrepo "github.com/speakeasy-api/gram/server/internal/directory/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	directoryRoleMappingSourceGroup     = "group"
	directoryRoleMappingSourceAttribute = "attribute"

	// maxDirectoryAttributeOptionsPerKey caps the attribute keys offered as
	// mapping sources. A key with more distinct values than this holds
	// per-person data (emails, employee ids), so it is left out of the list.
	// SetDirectoryRoleMapping still accepts any existing key and value.
	maxDirectoryAttributeOptionsPerKey = 100
)

// ListDirectoryRoleMappings returns the organization's live mappings together
// with the directory groups and attribute values an admin can map.
func (s *Service) ListDirectoryRoleMappings(ctx context.Context, _ *gen.ListDirectoryRoleMappingsPayload) (*gen.ListDirectoryRoleMappingsResult, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "missing auth context").LogError(ctx, s.logger)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
	)

	dirQueries := directoryrepo.New(s.db)

	groupRows, err := dirQueries.ListActiveDirectoryGroups(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list directory groups").LogError(ctx, s.logger)
	}
	groups := make([]*gen.DirectoryGroupOption, 0, len(groupRows))
	for _, row := range groupRows {
		groups = append(groups, &gen.DirectoryGroupOption{
			ID:          row.ID.String(),
			Name:        row.Name,
			MemberCount: row.MemberCount,
		})
	}

	attributeRows, err := dirQueries.ListMappableDirectoryAttributeValues(ctx, directoryrepo.ListMappableDirectoryAttributeValuesParams{
		OrganizationID:  ac.ActiveOrganizationID,
		MaxValuesPerKey: maxDirectoryAttributeOptionsPerKey,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list directory attribute values").LogError(ctx, s.logger)
	}
	attributes := make([]*gen.DirectoryAttributeOption, 0, len(attributeRows))
	for _, row := range attributeRows {
		attributes = append(attributes, &gen.DirectoryAttributeOption{
			Key:         row.AttributeKey,
			Value:       row.AttributeValue,
			MemberCount: row.MemberCount,
		})
	}

	mappingRows, err := repo.New(s.db).ListDirectoryRoleMappings(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list directory role mappings").LogError(ctx, s.logger)
	}
	mappings := make([]*gen.DirectoryRoleMapping, 0, len(mappingRows))
	for _, row := range mappingRows {
		mappings = append(mappings, &gen.DirectoryRoleMapping{
			ID:                 row.ID.String(),
			SourceKind:         row.SourceKind,
			DirectoryGroupID:   conv.FromNullableUUID(row.DirectoryGroupID),
			DirectoryGroupName: conv.FromPGText[string](row.DirectoryGroupName),
			AttributeKey:       conv.FromPGText[string](row.AttributeKey),
			AttributeValue:     conv.FromPGText[string](row.AttributeValue),
			RoleUrn:            row.RoleUrn,
			CreatedAt:          conv.FromPGTimestamptz(row.CreatedAt),
			UpdatedAt:          conv.FromPGTimestamptz(row.UpdatedAt),
		})
	}

	return &gen.ListDirectoryRoleMappingsResult{
		Groups:     groups,
		Attributes: attributes,
		Mappings:   mappings,
	}, nil
}

// SyncDirectoryGroups reads every group from the organization's linked WorkOS
// directories and saves new or changed ones, so admins can map groups that
// no directory event has delivered yet.
func (s *Service) SyncDirectoryGroups(ctx context.Context, _ *gen.SyncDirectoryGroupsPayload) (*gen.SyncDirectoryGroupsResult, error) {
	ac, workosOrgID, err := s.roleOrgContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	directories, err := s.roleMgr.roles.ListDirectories(ctx, workosOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "list directories").LogError(ctx, s.logger)
	}

	dirQueries := directoryrepo.New(s.db)
	groupCount := 0
	for _, directory := range directories {
		if !workos.HasActiveDirectory([]workos.Directory{directory}) {
			continue
		}

		groups, err := s.roleMgr.roles.ListDirectoryGroups(ctx, directory.ID)
		if err != nil {
			return nil, oops.E(oops.CodeGatewayError, err, "list directory groups").LogError(ctx, s.logger)
		}
		groupCount += len(groups)

		for _, group := range groups {
			attributes := []byte(group.RawAttributes)
			if len(attributes) == 0 || string(attributes) == "null" {
				attributes = []byte("{}")
			}
			if _, err := dirQueries.UpsertListedDirectoryGroup(ctx, directoryrepo.UpsertListedDirectoryGroupParams{
				OrganizationID:         ac.ActiveOrganizationID,
				WorkosDirectoryGroupID: group.ID,
				Name:                   group.Name,
				Attributes:             attributes,
				WorkosCreatedAt:        conv.ToPGTimestamptz(workosTimeOrNow(group.CreatedAt)),
				WorkosUpdatedAt:        conv.ToPGTimestamptz(workosTimeOrNow(group.UpdatedAt)),
			}); err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "save directory group").LogError(ctx, s.logger)
			}
		}
	}

	return &gen.SyncDirectoryGroupsResult{GroupCount: groupCount}, nil
}

// SetDirectoryRoleMapping maps a directory group or attribute value to a role,
// replacing the role it was mapped to before.
func (s *Service) SetDirectoryRoleMapping(ctx context.Context, payload *gen.SetDirectoryRoleMappingPayload) (*gen.DirectoryRoleMapping, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "missing auth context").LogError(ctx, s.logger)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
	)

	var groupID uuid.NullUUID
	switch payload.SourceKind {
	case directoryRoleMappingSourceGroup:
		if payload.DirectoryGroupID == nil || payload.AttributeKey != nil || payload.AttributeValue != nil {
			return nil, oops.E(oops.CodeBadRequest, nil, "a group mapping needs directory_group_id and no attribute fields").LogError(ctx, s.logger)
		}
		id, err := uuid.Parse(*payload.DirectoryGroupID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid directory group ID").LogError(ctx, s.logger)
		}
		groupID = uuid.NullUUID{UUID: id, Valid: true}
	case directoryRoleMappingSourceAttribute:
		if payload.AttributeKey == nil || payload.AttributeValue == nil || payload.DirectoryGroupID != nil {
			return nil, oops.E(oops.CodeBadRequest, nil, "an attribute mapping needs attribute_key and attribute_value and no directory_group_id").LogError(ctx, s.logger)
		}
	default:
		return nil, oops.E(oops.CodeBadRequest, nil, "unknown source kind").LogError(ctx, s.logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	queries := repo.New(dbtx)

	roles, err := queries.ListActiveOrganizationRoles(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list roles").LogError(ctx, s.logger)
	}
	roleFound := false
	for _, role := range roles {
		if role.RoleUrn == payload.RoleUrn {
			roleFound = true
			break
		}
	}
	if !roleFound {
		return nil, oops.E(oops.CodeNotFound, ErrRoleNotFound, "role not found").LogError(ctx, s.logger)
	}

	// lockKey serializes writes for this group or attribute value.
	var sourceLabel, lockKey string
	var groupName *string
	if groupID.Valid {
		name, err := queries.GetActiveDirectoryGroupName(ctx, repo.GetActiveDirectoryGroupNameParams{
			ID:             groupID.UUID,
			OrganizationID: ac.ActiveOrganizationID,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil, oops.E(oops.CodeNotFound, err, "directory group not found").LogError(ctx, s.logger)
		case err != nil:
			return nil, oops.E(oops.CodeUnexpected, err, "get directory group").LogError(ctx, s.logger)
		}
		sourceLabel = name
		groupName = &name
		lockKey = ac.ActiveOrganizationID + ":group:" + groupID.UUID.String()
	} else {
		exists, err := directoryrepo.New(dbtx).DirectoryAttributeValueExists(ctx, directoryrepo.DirectoryAttributeValueExistsParams{
			OrganizationID: ac.ActiveOrganizationID,
			AttributeKey:   []byte(*payload.AttributeKey),
			AttributeValue: []byte(*payload.AttributeValue),
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "check directory attribute value").LogError(ctx, s.logger)
		}
		if !exists {
			return nil, oops.E(oops.CodeNotFound, nil, "no directory user has this attribute value").LogError(ctx, s.logger)
		}
		sourceLabel = *payload.AttributeKey + "=" + *payload.AttributeValue
		lockKey = ac.ActiveOrganizationID + ":attr:" + sourceLabel
	}

	if err := queries.LockDirectoryRoleMappingSource(ctx, lockKey); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock directory role mapping source").LogError(ctx, s.logger)
	}

	var previousRoleURN *string
	previous, err := queries.GetLiveDirectoryRoleMappingRoleForSource(ctx, repo.GetLiveDirectoryRoleMappingRoleForSourceParams{
		OrganizationID:   ac.ActiveOrganizationID,
		DirectoryGroupID: groupID,
		AttributeKey:     conv.PtrToPGText(payload.AttributeKey),
		AttributeValue:   conv.PtrToPGText(payload.AttributeValue),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "get current directory role mapping").LogError(ctx, s.logger)
	default:
		previousRoleURN = &previous
	}

	var (
		mappingID uuid.UUID
		createdAt string
		updatedAt string
	)
	if groupID.Valid {
		row, err := queries.UpsertDirectoryGroupRoleMapping(ctx, repo.UpsertDirectoryGroupRoleMappingParams{
			OrganizationID:   ac.ActiveOrganizationID,
			DirectoryGroupID: groupID,
			RoleUrn:          payload.RoleUrn,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "save directory group role mapping").LogError(ctx, s.logger)
		}
		mappingID, createdAt, updatedAt = row.ID, conv.FromPGTimestamptz(row.CreatedAt), conv.FromPGTimestamptz(row.UpdatedAt)
	} else {
		row, err := queries.UpsertDirectoryAttributeRoleMapping(ctx, repo.UpsertDirectoryAttributeRoleMappingParams{
			OrganizationID: ac.ActiveOrganizationID,
			AttributeKey:   conv.PtrToPGText(payload.AttributeKey),
			AttributeValue: conv.PtrToPGText(payload.AttributeValue),
			RoleUrn:        payload.RoleUrn,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "save directory attribute role mapping").LogError(ctx, s.logger)
		}
		mappingID, createdAt, updatedAt = row.ID, conv.FromPGTimestamptz(row.CreatedAt), conv.FromPGTimestamptz(row.UpdatedAt)
	}

	if err := s.audit.LogDirectoryRoleMappingSet(ctx, dbtx, audit.LogDirectoryRoleMappingSetEvent{
		OrganizationID:   ac.ActiveOrganizationID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
		ActorDisplayName: ac.Email,
		ActorSlug:        nil,
		MappingURN:       urn.NewDirectoryRoleMapping(mappingID),
		SourceLabel:      sourceLabel,
		RoleURN:          payload.RoleUrn,
		PreviousRoleURN:  previousRoleURN,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log directory role mapping set").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit directory role mapping").LogError(ctx, s.logger)
	}

	return &gen.DirectoryRoleMapping{
		ID:                 mappingID.String(),
		SourceKind:         payload.SourceKind,
		DirectoryGroupID:   conv.FromNullableUUID(groupID),
		DirectoryGroupName: groupName,
		AttributeKey:       payload.AttributeKey,
		AttributeValue:     payload.AttributeValue,
		RoleUrn:            payload.RoleUrn,
		CreatedAt:          createdAt,
		UpdatedAt:          updatedAt,
	}, nil
}

// DeleteDirectoryRoleMapping removes a mapping. Members keep any role they
// hold directly; they only lose the role this mapping granted.
func (s *Service) DeleteDirectoryRoleMapping(ctx context.Context, payload *gen.DeleteDirectoryRoleMappingPayload) error {
	ac, err := s.authContext(ctx)
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "missing auth context").LogError(ctx, s.logger)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return err
	}
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
	)

	mappingID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid directory role mapping ID").LogError(ctx, s.logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	queries := repo.New(dbtx)

	mapping, err := queries.GetDirectoryRoleMapping(ctx, repo.GetDirectoryRoleMappingParams{
		ID:             mappingID,
		OrganizationID: ac.ActiveOrganizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return oops.E(oops.CodeNotFound, err, "directory role mapping not found").LogError(ctx, s.logger)
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "get directory role mapping").LogError(ctx, s.logger)
	}

	sourceLabel := mapping.AttributeKey.String + "=" + mapping.AttributeValue.String
	if mapping.DirectoryGroupID.Valid {
		// A group deleted in the directory keeps its mapping row, so fall back
		// to the group ID when its name is gone.
		sourceLabel = mapping.DirectoryGroupID.UUID.String()
		name, err := queries.GetActiveDirectoryGroupName(ctx, repo.GetActiveDirectoryGroupNameParams{
			ID:             mapping.DirectoryGroupID.UUID,
			OrganizationID: ac.ActiveOrganizationID,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return oops.E(oops.CodeUnexpected, err, "get directory group").LogError(ctx, s.logger)
		default:
			sourceLabel = name
		}
	}

	deleted, err := queries.DeleteDirectoryRoleMapping(ctx, repo.DeleteDirectoryRoleMappingParams{
		ID:             mappingID,
		OrganizationID: ac.ActiveOrganizationID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete directory role mapping").LogError(ctx, s.logger)
	}
	if deleted == 0 {
		return oops.E(oops.CodeNotFound, nil, "directory role mapping not found").LogError(ctx, s.logger)
	}

	if err := s.audit.LogDirectoryRoleMappingDelete(ctx, dbtx, audit.LogDirectoryRoleMappingDeleteEvent{
		OrganizationID:   ac.ActiveOrganizationID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
		ActorDisplayName: ac.Email,
		ActorSlug:        nil,
		MappingURN:       urn.NewDirectoryRoleMapping(mappingID),
		SourceLabel:      sourceLabel,
		RoleURN:          mapping.RoleUrn,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log directory role mapping delete").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit directory role mapping delete").LogError(ctx, s.logger)
	}

	return nil
}
