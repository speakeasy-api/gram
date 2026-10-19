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
	"github.com/speakeasy-api/gram/server/internal/directoryreview"
	reviewrepo "github.com/speakeasy-api/gram/server/internal/directoryreview/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	directoryReviewDecisionApprove   = "approve"
	directoryReviewDecisionDismiss   = "dismiss"
	directoryReviewDecisionOptOutKey = "opt_out_key"
)

// ListDirectoryReviewItems lists the pending directory review items, each with
// the proposal bulk setup would make for its source. It writes nothing.
func (s *Service) ListDirectoryReviewItems(ctx context.Context, _ *gen.ListDirectoryReviewItemsPayload) (*gen.ListDirectoryReviewItemsResult, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "missing auth context").LogError(ctx, s.logger)
	}
	// Admin only, as for bulk setup: attribute values can carry personal
	// directory data.
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
	)
	orgID := ac.ActiveOrganizationID

	rows, err := reviewrepo.New(s.db).ListPendingDirectoryReviewItems(ctx, orgID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list directory review items").LogError(ctx, s.logger)
	}
	if len(rows) == 0 {
		return &gen.ListDirectoryReviewItemsResult{Items: []*gen.DirectoryReviewItem{}}, nil
	}
	index, err := s.directorySourceRoleIndex(ctx, orgID)
	if err != nil {
		return nil, err
	}

	items := make([]*gen.DirectoryReviewItem, 0, len(rows))
	for _, row := range rows {
		var preview *gen.DirectoryRoleSetupPreviewItem
		if row.DirectoryGroupID.Valid {
			groupID := row.DirectoryGroupID.UUID.String()
			name := row.DirectoryGroupName.String
			roleURNs := sortedUnique(index.existing[directorySourceKey{groupID: row.DirectoryGroupID.UUID, key: "", value: ""}])
			preview = &gen.DirectoryRoleSetupPreviewItem{
				Source: &gen.DirectoryRoleSetupSource{
					SourceKind: directoryRoleMappingSourceGroup, DirectoryGroupID: &groupID,
					AttributeKey: nil, AttributeValue: nil,
				},
				SourceName:       name,
				DirectoryPeople:  row.DirectoryPeople,
				ExistingRoleUrns: roleURNs,
				ProposedRoles:    proposeDirectorySourceRoles(name, roleURNs, index.roleByURN, index.roleBySlug),
				Version:          directorySourceVersion(directoryRoleMappingSourceGroup, groupID, name, roleURNs),
			}
		} else {
			key, value := row.AttributeKey.String, row.AttributeValue.String
			sourceName := key + "=" + value
			roleURNs := sortedUnique(index.existing[directorySourceKey{groupID: uuid.Nil, key: key, value: value}])
			preview = &gen.DirectoryRoleSetupPreviewItem{
				Source: &gen.DirectoryRoleSetupSource{
					SourceKind: directoryRoleMappingSourceAttribute, DirectoryGroupID: nil,
					AttributeKey: &key, AttributeValue: &value,
				},
				SourceName:       sourceName,
				DirectoryPeople:  row.DirectoryPeople,
				ExistingRoleUrns: roleURNs,
				ProposedRoles:    proposeDirectorySourceRoles(value, roleURNs, index.roleByURN, index.roleBySlug),
				Version:          directorySourceVersion(directoryRoleMappingSourceAttribute, key+"\x00"+value, sourceName, roleURNs),
			}
		}
		items = append(items, &gen.DirectoryReviewItem{
			ID:         row.ID.String(),
			Kind:       row.Kind,
			Occurrence: row.Occurrence,
			CreatedAt:  conv.FromPGTimestamptz(row.CreatedAt),
			Item:       preview,
		})
	}
	return &gen.ListDirectoryReviewItemsResult{Items: items}, nil
}

// ResolveDirectoryReviewItem records an admin's decision on one pending item,
// and for an approval writes the source's mappings in the same transaction.
func (s *Service) ResolveDirectoryReviewItem(ctx context.Context, payload *gen.ResolveDirectoryReviewItemPayload) (*gen.ResolveDirectoryReviewItemResult, error) {
	ac, workosOrgID, err := s.roleOrgContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
	)
	orgID := ac.ActiveOrganizationID

	itemID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid directory review item ID").LogWarn(ctx, s.logger)
	}

	// Read the item's source first: its lock is taken before the item's row
	// lock, the order mapping writes take them in.
	item, err := reviewrepo.New(s.db).GetDirectoryReviewItem(ctx, reviewrepo.GetDirectoryReviewItemParams{OrganizationID: orgID, ID: itemID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "directory review item not found").LogWarn(ctx, s.logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "get directory review item").LogError(ctx, s.logger)
	}
	// An item someone already resolved is a conflict whatever the request
	// says, so a stale client hears that before any validation error. The
	// locked read below rechecks it.
	if item.State != directoryreview.StatePending {
		return nil, errDirectoryReviewItemResolved(ctx, s, nil)
	}
	for _, role := range payload.Roles {
		if role == nil || (role.RoleUrn == nil) == (role.CreateName == nil) {
			return nil, oops.E(oops.CodeBadRequest, nil, "each role needs exactly one of role_urn or create_name").LogWarn(ctx, s.logger)
		}
	}
	if err := validateDirectoryReviewDecision(item.Kind, payload); err != nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "%s", err.Error()).LogWarn(ctx, s.logger)
	}
	source := directoryReviewItemSource(item.DirectoryGroupID, item.AttributeKey.String, item.AttributeValue.String)
	lockKey := directoryRoleSourceLockKey(orgID, item.DirectoryGroupID, source.AttributeKey, source.AttributeValue)

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	if err := s.lockDirectoryRoleSources(ctx, dbtx, []string{lockKey}); err != nil {
		return nil, err
	}
	if err := s.requireLiveOrgAdminWithDBTX(ctx, ac, dbtx); err != nil {
		return nil, err
	}
	reviews := reviewrepo.New(dbtx)
	if _, err := reviews.GetPendingDirectoryReviewItemForUpdate(ctx, reviewrepo.GetPendingDirectoryReviewItemForUpdateParams{OrganizationID: orgID, ID: itemID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errDirectoryReviewItemResolved(ctx, s, err)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "lock directory review item").LogError(ctx, s.logger)
	}

	sourceLabel, err := s.directoryReviewSourceLabel(ctx, dbtx, orgID, item.DirectoryGroupID, source)
	if err != nil {
		return nil, err
	}

	// Settle the item before writing mappings: the mapping write supersedes
	// pending items its change resolves, and this one is resolved by the
	// admin, not superseded.
	state := directoryreview.StateDismissed
	if payload.Decision == directoryReviewDecisionApprove {
		state = directoryreview.StateApproved
	}
	if _, err := reviews.ResolveDirectoryReviewItem(ctx, reviewrepo.ResolveDirectoryReviewItemParams{
		State: state, ResolvedByUserID: conv.ToPGText(ac.UserID), OrganizationID: orgID, ID: itemID,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "resolve directory review item").LogError(ctx, s.logger)
	}

	actor := urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID)
	itemURN := urn.NewDirectoryReviewItem(itemID)
	applied := &directoryRoleSetupApplied{createdRoles: []*gen.Role{}, mappings: []*gen.DirectoryRoleMapping{}, reconciliations: nil}
	switch payload.Decision {
	case directoryReviewDecisionApprove:
		// Approving a missing source removes its mappings: an empty set.
		roles := payload.Roles
		if item.Kind == directoryreview.KindSourceMissing {
			roles = []*gen.ApplyDirectoryRoleSetupRole{}
		}
		applied, err = s.applyDirectoryRoleSetupTx(ctx, dbtx, ac, workosOrgID, []directoryRoleSetupItem{{
			item:    &gen.ApplyDirectoryRoleSetupItem{Source: source, Version: conv.PtrValOr(payload.Version, ""), Roles: roles},
			groupID: item.DirectoryGroupID,
			lockKey: lockKey,
		}}, payload.GrantSet)
		if err != nil {
			return nil, err
		}
		if item.Kind == directoryreview.KindSourceMissing {
			// The mappings were removed, so nothing is returned as written.
			applied.mappings = []*gen.DirectoryRoleMapping{}
		}
		err = s.audit.LogDirectoryReviewItemApprove(ctx, dbtx, audit.LogDirectoryReviewItemApproveEvent{
			OrganizationID: orgID, Actor: actor, ActorDisplayName: ac.Email, ActorSlug: nil,
			ItemURN: itemURN, SourceLabel: sourceLabel, Kind: item.Kind, Occurrence: item.Occurrence,
		})
	case directoryReviewDecisionOptOutKey:
		key := item.AttributeKey.String
		if err := reviews.InsertDirectoryReviewAttributeOptOut(ctx, reviewrepo.InsertDirectoryReviewAttributeOptOutParams{OrganizationID: orgID, AttributeKey: key}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "opt out of directory attribute key").LogError(ctx, s.logger)
		}
		if _, err := directoryreview.SupersedeAttributeKey(ctx, dbtx, orgID, key); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "settle directory review items for opted-out key").LogError(ctx, s.logger)
		}
		err = s.audit.LogDirectoryReviewItemOptOut(ctx, dbtx, audit.LogDirectoryReviewItemOptOutEvent{
			OrganizationID: orgID, Actor: actor, ActorDisplayName: ac.Email, ActorSlug: nil,
			ItemURN: itemURN, SourceLabel: sourceLabel, Kind: item.Kind, Occurrence: item.Occurrence, AttributeKey: key,
		})
	default:
		err = s.audit.LogDirectoryReviewItemDismiss(ctx, dbtx, audit.LogDirectoryReviewItemDismissEvent{
			OrganizationID: orgID, Actor: actor, ActorDisplayName: ac.Email, ActorSlug: nil,
			ItemURN: itemURN, SourceLabel: sourceLabel, Kind: item.Kind, Occurrence: item.Occurrence,
		})
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log directory review decision").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit directory review decision").LogError(ctx, s.logger)
	}

	return &gen.ResolveDirectoryReviewItemResult{
		State:        state,
		CreatedRoles: applied.createdRoles,
		Mappings:     applied.mappings,
		SideEffects:  s.reconcileDirectorySetupRoles(ctx, applied),
	}, nil
}

func errDirectoryReviewItemResolved(ctx context.Context, s *Service, cause error) error {
	return oops.E(oops.CodeConflict, cause, "this directory change has already been resolved; refresh the review list").LogWarn(ctx, s.logger)
}

// validateDirectoryReviewDecision checks a decision fits the item's kind.
func validateDirectoryReviewDecision(kind string, payload *gen.ResolveDirectoryReviewItemPayload) error {
	switch payload.Decision {
	case directoryReviewDecisionApprove:
		if payload.Version == nil || *payload.Version == "" {
			return errors.New("approve needs the item version")
		}
		if kind == directoryreview.KindSourceMissing {
			if len(payload.Roles) > 0 {
				return errors.New("approving a missing source removes its mappings; send no roles")
			}
			return nil
		}
		if len(payload.Roles) == 0 {
			return errors.New("approving a new source needs at least one role; dismiss it to leave it unmapped")
		}
	case directoryReviewDecisionDismiss:
		if len(payload.Roles) > 0 {
			return errors.New("dismiss takes no roles")
		}
	case directoryReviewDecisionOptOutKey:
		if kind != directoryreview.KindNewAttributeValue {
			return errors.New("only a new attribute value can opt its key out")
		}
		if len(payload.Roles) > 0 {
			return errors.New("opt_out_key takes no roles")
		}
	default:
		return errors.New("unknown decision")
	}
	return nil
}

func directoryReviewItemSource(groupID uuid.NullUUID, attributeKey, attributeValue string) *gen.DirectoryRoleSetupSource {
	if groupID.Valid {
		id := groupID.UUID.String()
		return &gen.DirectoryRoleSetupSource{SourceKind: directoryRoleMappingSourceGroup, DirectoryGroupID: &id, AttributeKey: nil, AttributeValue: nil}
	}
	return &gen.DirectoryRoleSetupSource{SourceKind: directoryRoleMappingSourceAttribute, DirectoryGroupID: nil, AttributeKey: &attributeKey, AttributeValue: &attributeValue}
}

// directoryReviewSourceLabel names a source for the audit log, as mapping
// audit entries do: the group's name, or key=value.
func (s *Service) directoryReviewSourceLabel(ctx context.Context, dbtx pgx.Tx, orgID string, groupID uuid.NullUUID, source *gen.DirectoryRoleSetupSource) (string, error) {
	if !groupID.Valid {
		return *source.AttributeKey + "=" + *source.AttributeValue, nil
	}
	group, err := repo.New(dbtx).GetDirectoryRoleMappingGroupSource(ctx, repo.GetDirectoryRoleMappingGroupSourceParams{ID: groupID.UUID, OrganizationID: orgID})
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "get directory group").LogError(ctx, s.logger)
	}
	return group.Name, nil
}
