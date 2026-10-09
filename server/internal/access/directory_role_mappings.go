package access

import (
	"context"
	"errors"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

	// maxDirectoryAttributeOptionsPerKey keeps the option list usable: a key
	// with more distinct values than this (emails, employee ids) is no use as
	// a mapping source, so it is left out. It is not a privacy boundary; the
	// admin-only scope on ListDirectoryRoleMappings is. SetDirectoryRoleMappings
	// still accepts any existing key and value.
	maxDirectoryAttributeOptionsPerKey = 100
)

// ListDirectoryRoleMappings returns the organization's live mappings together
// with the directory groups and attribute values an admin can map.
func (s *Service) ListDirectoryRoleMappings(ctx context.Context, _ *gen.ListDirectoryRoleMappingsPayload) (*gen.ListDirectoryRoleMappingsResult, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "missing auth context").LogError(ctx, s.logger)
	}
	// Admin only: attribute values can carry personal directory data.
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
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

// SetDirectoryRoleMapping keeps already-open dashboard tabs working during rollout.
func (s *Service) SetDirectoryRoleMapping(ctx context.Context, payload *gen.SetDirectoryRoleMappingPayload) (*gen.DirectoryRoleMapping, error) {
	mappings, err := s.setDirectoryRoleMappings(ctx, &gen.SetDirectoryRoleMappingsPayload{
		SourceKind: payload.SourceKind, DirectoryGroupID: payload.DirectoryGroupID,
		AttributeKey: payload.AttributeKey, AttributeValue: payload.AttributeValue,
		RoleUrns: []string{payload.RoleUrn}, SessionToken: payload.SessionToken, ApikeyToken: payload.ApikeyToken,
	}, true)
	if err != nil {
		return nil, err
	}
	return mappings[0], nil
}

// SetDirectoryRoleMappings replaces the roles granted by one directory source.
func (s *Service) SetDirectoryRoleMappings(ctx context.Context, payload *gen.SetDirectoryRoleMappingsPayload) ([]*gen.DirectoryRoleMapping, error) {
	return s.setDirectoryRoleMappings(ctx, payload, false)
}

func (s *Service) setDirectoryRoleMappings(ctx context.Context, payload *gen.SetDirectoryRoleMappingsPayload, legacy bool) ([]*gen.DirectoryRoleMapping, error) {
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

	lockKey := ac.ActiveOrganizationID + ":attr:" + conv.PtrValOr(payload.AttributeKey, "") + "=" + conv.PtrValOr(payload.AttributeValue, "")
	if groupID.Valid {
		lockKey = ac.ActiveOrganizationID + ":group:" + groupID.UUID.String()
	}
	if err := queries.LockDirectoryRoleMappingSource(ctx, lockKey); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock directory role mapping source").LogError(ctx, s.logger)
	}
	if err := s.requireLiveOrgAdminWithDBTX(ctx, ac, dbtx); err != nil {
		return nil, err
	}

	roleURNs := slices.Clone(payload.RoleUrns)
	slices.Sort(roleURNs)
	roleURNs = slices.Compact(roleURNs)
	roles, err := queries.ListLiveDirectoryMappingRoles(ctx, repo.ListLiveDirectoryMappingRolesParams{
		OrganizationID: ac.ActiveOrganizationID, RoleUrns: roleURNs,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list roles").LogError(ctx, s.logger)
	}
	for _, roleURN := range roleURNs {
		if !slices.Contains(roles, roleURN) {
			return nil, oops.E(oops.CodeNotFound, ErrRoleNotFound, "role not found").LogWarn(ctx, s.logger)
		}
	}

	var sourceLabel string
	var groupName *string
	if groupID.Valid {
		group, err := queries.GetDirectoryRoleMappingGroupSource(ctx, repo.GetDirectoryRoleMappingGroupSourceParams{
			ID:             groupID.UUID,
			OrganizationID: ac.ActiveOrganizationID,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil, oops.E(oops.CodeNotFound, err, "directory group not found").LogError(ctx, s.logger)
		case err != nil:
			return nil, oops.E(oops.CodeUnexpected, err, "get directory group").LogError(ctx, s.logger)
		}
		if len(roleURNs) > 0 && (group.Deleted || group.WorkosDeleted) {
			return nil, oops.E(oops.CodeNotFound, nil, "directory group not found").LogWarn(ctx, s.logger)
		}
		sourceLabel = group.Name
		groupName = &group.Name
	} else {
		exists, err := directoryrepo.New(dbtx).DirectoryAttributeValueExists(ctx, directoryrepo.DirectoryAttributeValueExistsParams{
			OrganizationID: ac.ActiveOrganizationID,
			AttributeKey:   []byte(*payload.AttributeKey),
			AttributeValue: []byte(*payload.AttributeValue),
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "check directory attribute value").LogError(ctx, s.logger)
		}
		if len(roleURNs) > 0 && !exists {
			return nil, oops.E(oops.CodeNotFound, nil, "no directory user has this attribute value").LogWarn(ctx, s.logger)
		}
		sourceLabel = *payload.AttributeKey + "=" + *payload.AttributeValue
	}

	current, err := queries.ListLiveDirectoryRoleMappingsForSource(ctx, repo.ListLiveDirectoryRoleMappingsForSourceParams{
		OrganizationID:   ac.ActiveOrganizationID,
		DirectoryGroupID: groupID,
		AttributeKey:     conv.PtrToPGText(payload.AttributeKey),
		AttributeValue:   conv.PtrToPGText(payload.AttributeValue),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list current directory role mappings").LogError(ctx, s.logger)
	}

	// Check under the source lock, before changing rows or emitting audit events.
	// A nil slice means omitted; an explicitly empty slice requires no mappings.
	if payload.ExpectedRoleUrns != nil {
		expected := slices.Clone(payload.ExpectedRoleUrns)
		slices.Sort(expected)
		expected = slices.Compact(expected)
		actual := make([]string, 0, len(current))
		for _, row := range current {
			actual = append(actual, row.RoleUrn)
		}
		slices.Sort(actual)
		actual = slices.Compact(actual)
		if !slices.Equal(expected, actual) {
			return nil, oops.E(oops.CodeConflict, nil, "directory role mappings changed; refresh before retrying").LogWarn(ctx, s.logger)
		}
	}

	if legacy && len(current) > 1 {
		return nil, oops.E(oops.CodeConflict, nil, "this source grants multiple roles; refresh the dashboard to edit its role set").LogWarn(ctx, s.logger)
	}

	// Remove first so singleton replacements work while the source-only indexes
	// are retained during rollout. Unchanged rows keep their identity and timestamps.
	for _, row := range current {
		if slices.Contains(roleURNs, row.RoleUrn) {
			continue
		}
		if _, err := queries.DeleteDirectoryRoleMapping(ctx, repo.DeleteDirectoryRoleMappingParams{
			ID: row.ID, OrganizationID: ac.ActiveOrganizationID,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "remove directory role mapping").LogError(ctx, s.logger)
		}
		if err := s.audit.LogDirectoryRoleMappingDelete(ctx, dbtx, audit.LogDirectoryRoleMappingDeleteEvent{
			OrganizationID: ac.ActiveOrganizationID,
			Actor:          urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID), ActorDisplayName: ac.Email, ActorSlug: nil,
			MappingURN: urn.NewDirectoryRoleMapping(row.ID), SourceLabel: sourceLabel, RoleURN: row.RoleUrn,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "log directory role mapping removal").LogError(ctx, s.logger)
		}
	}

	mappings := make([]*gen.DirectoryRoleMapping, 0, len(roleURNs))
	for _, roleURN := range roleURNs {
		var mappingID uuid.UUID
		var createdAt, updatedAt string
		index := slices.IndexFunc(current, func(row repo.ListLiveDirectoryRoleMappingsForSourceRow) bool { return row.RoleUrn == roleURN })
		if index >= 0 {
			row := current[index]
			mappingID, createdAt, updatedAt = row.ID, conv.FromPGTimestamptz(row.CreatedAt), conv.FromPGTimestamptz(row.UpdatedAt)
		} else {
			if groupID.Valid {
				row, err := queries.InsertDirectoryGroupRoleMapping(ctx, repo.InsertDirectoryGroupRoleMappingParams{
					OrganizationID: ac.ActiveOrganizationID, DirectoryGroupID: groupID, RoleUrn: roleURN,
				})
				if err != nil {
					if isLegacyDirectoryMappingConflict(err) {
						return nil, oops.E(oops.CodeConflict, err, "directory role sets require the remaining schema rollout").LogWarn(ctx, s.logger)
					}
					return nil, oops.E(oops.CodeUnexpected, err, "add directory group role mapping").LogError(ctx, s.logger)
				}
				mappingID, createdAt, updatedAt = row.ID, conv.FromPGTimestamptz(row.CreatedAt), conv.FromPGTimestamptz(row.UpdatedAt)
			} else {
				row, err := queries.InsertDirectoryAttributeRoleMapping(ctx, repo.InsertDirectoryAttributeRoleMappingParams{
					OrganizationID: ac.ActiveOrganizationID, AttributeKey: conv.PtrToPGText(payload.AttributeKey),
					AttributeValue: conv.PtrToPGText(payload.AttributeValue), RoleUrn: roleURN,
				})
				if err != nil {
					if isLegacyDirectoryMappingConflict(err) {
						return nil, oops.E(oops.CodeConflict, err, "directory role sets require the remaining schema rollout").LogWarn(ctx, s.logger)
					}
					return nil, oops.E(oops.CodeUnexpected, err, "add directory attribute role mapping").LogError(ctx, s.logger)
				}
				mappingID, createdAt, updatedAt = row.ID, conv.FromPGTimestamptz(row.CreatedAt), conv.FromPGTimestamptz(row.UpdatedAt)
			}
			if err := s.audit.LogDirectoryRoleMappingSet(ctx, dbtx, audit.LogDirectoryRoleMappingSetEvent{
				OrganizationID: ac.ActiveOrganizationID,
				Actor:          urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID), ActorDisplayName: ac.Email, ActorSlug: nil,
				MappingURN: urn.NewDirectoryRoleMapping(mappingID), SourceLabel: sourceLabel, RoleURN: roleURN, PreviousRoleURN: nil,
			}); err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "log directory role mapping addition").LogError(ctx, s.logger)
			}
		}
		mappings = append(mappings, &gen.DirectoryRoleMapping{
			ID: mappingID.String(), SourceKind: payload.SourceKind, DirectoryGroupID: conv.FromNullableUUID(groupID),
			DirectoryGroupName: groupName, AttributeKey: payload.AttributeKey, AttributeValue: payload.AttributeValue,
			RoleUrn: roleURN, CreatedAt: createdAt, UpdatedAt: updatedAt,
		})
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit directory role mappings").LogError(ctx, s.logger)
	}
	return mappings, nil
}

func isLegacyDirectoryMappingConflict(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation &&
		(pgErr.ConstraintName == "directory_role_mappings_org_group_key" || pgErr.ConstraintName == "directory_role_mappings_org_attribute_key")
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

	lockKey := ac.ActiveOrganizationID + ":attr:" + mapping.AttributeKey.String + "=" + mapping.AttributeValue.String
	if mapping.DirectoryGroupID.Valid {
		lockKey = ac.ActiveOrganizationID + ":group:" + mapping.DirectoryGroupID.UUID.String()
	}
	if err := queries.LockDirectoryRoleMappingSource(ctx, lockKey); err != nil {
		return oops.E(oops.CodeUnexpected, err, "lock directory role mapping source").LogError(ctx, s.logger)
	}
	if err := s.requireLiveOrgAdminWithDBTX(ctx, ac, dbtx); err != nil {
		return err
	}
	mapping, err = queries.GetDirectoryRoleMapping(ctx, repo.GetDirectoryRoleMappingParams{
		ID: mappingID, OrganizationID: ac.ActiveOrganizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return oops.E(oops.CodeNotFound, err, "directory role mapping not found").LogWarn(ctx, s.logger)
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "get locked directory role mapping").LogError(ctx, s.logger)
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
