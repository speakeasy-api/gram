package access

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/trace"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	directoryrepo "github.com/speakeasy-api/gram/server/internal/directory/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	directoryRoleSetupActionReuse  = "reuse"
	directoryRoleSetupActionCreate = "create"

	// fallbackDirectoryRoleName names a new role for a source whose name has
	// no character a role name allows.
	fallbackDirectoryRoleName = "Directory role"
)

// invalidRoleNameChars is everything validRoleNamePattern refuses.
var invalidRoleNameChars = regexp.MustCompile(`[^A-Za-z0-9 _-]+`)

// directorySourceKey identifies one mapping source across the preview's reads.
type directorySourceKey struct {
	groupID uuid.UUID
	key     string
	value   string
}

// PreviewDirectoryRoleSetup proposes roles for every live directory source.
// It writes nothing.
func (s *Service) PreviewDirectoryRoleSetup(ctx context.Context, _ *gen.PreviewDirectoryRoleSetupPayload) (*gen.DirectoryRoleSetupPreviewResult, error) {
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
	orgID := ac.ActiveOrganizationID
	dirQueries := directoryrepo.New(s.db)

	groupRows, err := dirQueries.ListActiveDirectoryGroups(ctx, orgID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list directory groups").LogError(ctx, s.logger)
	}
	attributeRows, err := dirQueries.ListMappableDirectoryAttributeValues(ctx, directoryrepo.ListMappableDirectoryAttributeValuesParams{
		OrganizationID:  orgID,
		MaxValuesPerKey: maxDirectoryAttributeOptionsPerKey,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list directory attribute values").LogError(ctx, s.logger)
	}
	hiddenRows, err := dirQueries.ListUnmappableDirectoryAttributeKeys(ctx, directoryrepo.ListUnmappableDirectoryAttributeKeysParams{
		OrganizationID:  orgID,
		MaxValuesPerKey: maxDirectoryAttributeOptionsPerKey,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list unmappable directory attribute keys").LogError(ctx, s.logger)
	}
	index, err := s.directorySourceRoleIndex(ctx, orgID)
	if err != nil {
		return nil, err
	}
	existing, roleByURN, roleBySlug := index.existing, index.roleByURN, index.roleBySlug

	items := make([]*gen.DirectoryRoleSetupPreviewItem, 0, len(groupRows)+len(attributeRows))
	for _, row := range groupRows {
		groupID := row.ID.String()
		roleURNs := sortedUnique(existing[directorySourceKey{groupID: row.ID, key: "", value: ""}])
		items = append(items, &gen.DirectoryRoleSetupPreviewItem{
			Source: &gen.DirectoryRoleSetupSource{
				SourceKind:       directoryRoleMappingSourceGroup,
				DirectoryGroupID: &groupID,
				AttributeKey:     nil,
				AttributeValue:   nil,
			},
			SourceName:       row.Name,
			DirectoryPeople:  row.MemberCount,
			ExistingRoleUrns: roleURNs,
			ProposedRoles:    proposeDirectorySourceRoles(row.Name, roleURNs, roleByURN, roleBySlug),
			Version:          directorySourceVersion(directoryRoleMappingSourceGroup, groupID, row.Name, roleURNs),
		})
	}
	for _, row := range attributeRows {
		key, value := row.AttributeKey, row.AttributeValue
		roleURNs := sortedUnique(existing[directorySourceKey{groupID: uuid.Nil, key: key, value: value}])
		sourceName := key + "=" + value
		items = append(items, &gen.DirectoryRoleSetupPreviewItem{
			Source: &gen.DirectoryRoleSetupSource{
				SourceKind:       directoryRoleMappingSourceAttribute,
				DirectoryGroupID: nil,
				AttributeKey:     &key,
				AttributeValue:   &value,
			},
			SourceName:       sourceName,
			DirectoryPeople:  row.MemberCount,
			ExistingRoleUrns: roleURNs,
			// A new role is named after the value: "Sales", not "department=Sales".
			ProposedRoles: proposeDirectorySourceRoles(value, roleURNs, roleByURN, roleBySlug),
			Version:       directorySourceVersion(directoryRoleMappingSourceAttribute, key+"\x00"+value, sourceName, roleURNs),
		})
	}

	hidden := make([]*gen.DirectoryRoleSetupHiddenAttributeKey, 0, len(hiddenRows))
	for _, row := range hiddenRows {
		hidden = append(hidden, &gen.DirectoryRoleSetupHiddenAttributeKey{Key: row.AttributeKey, ValueCount: row.ValueCount})
	}

	return &gen.DirectoryRoleSetupPreviewResult{
		Items:                    items,
		HiddenAttributeKeys:      hidden,
		MaxValuesPerAttributeKey: maxDirectoryAttributeOptionsPerKey,
	}, nil
}

// directorySourceRoles indexes an organization's live mappings by source and
// its roles by URN and slug, for building proposals.
type directorySourceRoles struct {
	existing   map[directorySourceKey][]string
	roleByURN  map[string]*gen.Role
	roleBySlug map[string]*gen.Role
}

func (s *Service) directorySourceRoleIndex(ctx context.Context, orgID string) (directorySourceRoles, error) {
	var index directorySourceRoles
	mappingRows, err := repo.New(s.db).ListDirectoryRoleMappings(ctx, orgID)
	if err != nil {
		return index, oops.E(oops.CodeUnexpected, err, "list directory role mappings").LogError(ctx, s.logger)
	}
	roles, err := s.roleMgr.ListRoles(ctx, orgID)
	if err != nil {
		return index, err
	}

	index.existing = make(map[directorySourceKey][]string, len(mappingRows))
	for _, row := range mappingRows {
		var key directorySourceKey
		if row.DirectoryGroupID.Valid {
			key.groupID = row.DirectoryGroupID.UUID
		} else {
			key.key = row.AttributeKey.String
			key.value = row.AttributeValue.String
		}
		index.existing[key] = append(index.existing[key], row.RoleUrn)
	}
	index.roleByURN = make(map[string]*gen.Role, len(roles.Roles))
	index.roleBySlug = make(map[string]*gen.Role, len(roles.Roles))
	for _, role := range roles.Roles {
		index.roleByURN[role.PrincipalUrn] = role
		index.roleBySlug[role.Slug] = role
	}
	return index, nil
}

// proposeDirectorySourceRoles returns a mapped source's current roles, or for
// an unmapped source one role: the live role whose slug matches the source
// name, or a new role named after it.
func proposeDirectorySourceRoles(name string, roleURNs []string, roleByURN, roleBySlug map[string]*gen.Role) []*gen.DirectoryRoleSetupProposedRole {
	if len(roleURNs) > 0 {
		proposed := make([]*gen.DirectoryRoleSetupProposedRole, 0, len(roleURNs))
		for _, roleURN := range roleURNs {
			role, ok := roleByURN[roleURN]
			if !ok {
				// A deleted role stays mapped (and grants nothing) until
				// removed, so keep it in the set rather than drop it on apply.
				proposed = append(proposed, &gen.DirectoryRoleSetupProposedRole{
					RoleAction: directoryRoleSetupActionReuse, RoleID: nil, RoleUrn: &roleURN,
					RoleName: "Deleted role", CurrentGrants: []*gen.RoleGrant{},
				})
				continue
			}
			proposed = append(proposed, reuseProposal(role))
		}
		return proposed
	}

	roleName := directoryRoleName(name)
	if slug, err := slugify(roleName); err == nil {
		if role, ok := roleBySlug[slug]; ok {
			return []*gen.DirectoryRoleSetupProposedRole{reuseProposal(role)}
		}
	}
	return []*gen.DirectoryRoleSetupProposedRole{{
		RoleAction: directoryRoleSetupActionCreate, RoleID: nil, RoleUrn: nil,
		RoleName: roleName, CurrentGrants: []*gen.RoleGrant{},
	}}
}

func reuseProposal(role *gen.Role) *gen.DirectoryRoleSetupProposedRole {
	grants := role.Grants
	if grants == nil {
		grants = []*gen.RoleGrant{}
	}
	return &gen.DirectoryRoleSetupProposedRole{
		RoleAction: directoryRoleSetupActionReuse, RoleID: &role.ID, RoleUrn: &role.PrincipalUrn,
		RoleName: role.Name, CurrentGrants: grants,
	}
}

// directoryRoleName turns a source name into a valid role name: characters a
// role name refuses become spaces, so "R&D (EMEA)" proposes "R D EMEA". A name
// left with no letter or digit, such as "-", falls back to a generic name,
// since creating a role from it would be refused.
func directoryRoleName(sourceName string) string {
	name := strings.Join(strings.Fields(invalidRoleNameChars.ReplaceAllString(sourceName, " ")), " ")
	if _, err := slugify(name); err != nil {
		return fallbackDirectoryRoleName
	}
	return name
}

// directorySourceVersion fingerprints what a preview item was built from: the
// source, its name and its current role set. A role's grants are not part of
// it; apply re-resolves every role under the source lock instead.
func directorySourceVersion(sourceKind, sourceID, sourceName string, roleURNs []string) string {
	encoded, err := json.Marshal([]any{sourceKind, sourceID, sourceName, roleURNs})
	if err != nil {
		// Marshalling strings cannot fail.
		panic(err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:16])
}

func sortedUnique(values []string) []string {
	out := slices.Clone(values)
	if out == nil {
		out = []string{}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// directoryRoleSetupItem is one validated apply item.
type directoryRoleSetupItem struct {
	item    *gen.ApplyDirectoryRoleSetupItem
	groupID uuid.NullUUID
	lockKey string
}

// ApplyDirectoryRoleSetup creates the requested roles and replaces each
// source's role set in one transaction.
func (s *Service) ApplyDirectoryRoleSetup(ctx context.Context, payload *gen.ApplyDirectoryRoleSetupPayload) (*gen.ApplyDirectoryRoleSetupResult, error) {
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

	items := make([]directoryRoleSetupItem, 0, len(payload.Items))
	lockKeys := make([]string, 0, len(payload.Items))
	// Several sources may ask for the same new role. Names that differ but
	// make the same slug would silently become one role, so they are refused.
	newRoleNameBySlug := map[string]string{}
	for _, item := range payload.Items {
		if item == nil || item.Source == nil {
			return nil, oops.E(oops.CodeBadRequest, nil, "every item needs a source").LogError(ctx, s.logger)
		}
		groupID, err := s.directoryRoleSourceGroupID(ctx, item.Source.SourceKind, item.Source.DirectoryGroupID, item.Source.AttributeKey, item.Source.AttributeValue)
		if err != nil {
			return nil, err
		}
		lockKey := directoryRoleSourceLockKey(orgID, groupID, item.Source.AttributeKey, item.Source.AttributeValue)
		if slices.Contains(lockKeys, lockKey) {
			return nil, oops.E(oops.CodeBadRequest, nil, "a source may appear only once").LogError(ctx, s.logger)
		}
		// Checked before any role is created, so an over-cap item never
		// leaves new roles behind.
		distinctRoles := map[string]struct{}{}
		for _, role := range item.Roles {
			if role == nil || (role.RoleUrn == nil) == (role.CreateName == nil) {
				return nil, oops.E(oops.CodeBadRequest, nil, "each role needs exactly one of role_urn or create_name").LogError(ctx, s.logger)
			}
			if role.CreateName == nil {
				distinctRoles["urn:"+*role.RoleUrn] = struct{}{}
				continue
			}
			slug, err := slugify(*role.CreateName)
			if err != nil {
				return nil, err
			}
			distinctRoles["new:"+slug] = struct{}{}
			name := strings.TrimSpace(*role.CreateName)
			if earlier, ok := newRoleNameBySlug[slug]; ok && !strings.EqualFold(earlier, name) {
				return nil, oops.E(oops.CodeBadRequest, nil, "new roles %q and %q would have the same identifier; rename one", earlier, name).LogWarn(ctx, s.logger)
			}
			newRoleNameBySlug[slug] = name
		}
		if err := s.requireDirectoryRoleSetWithinCap(ctx, len(distinctRoles)); err != nil {
			return nil, err
		}
		lockKeys = append(lockKeys, lockKey)
		items = append(items, directoryRoleSetupItem{item: item, groupID: groupID, lockKey: lockKey})
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	if err := s.lockDirectoryRoleSources(ctx, dbtx, lockKeys); err != nil {
		return nil, err
	}
	if err := s.requireLiveOrgAdminWithDBTX(ctx, ac, dbtx); err != nil {
		return nil, err
	}

	applied, err := s.applyDirectoryRoleSetupTx(ctx, dbtx, ac, workosOrgID, items, payload.GrantSet)
	if err != nil {
		return nil, err
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit directory role setup").LogError(ctx, s.logger)
	}

	return &gen.ApplyDirectoryRoleSetupResult{
		CreatedRoles: applied.createdRoles,
		Mappings:     applied.mappings,
		SideEffects:  s.reconcileDirectorySetupRoles(ctx, applied),
	}, nil
}

// lockDirectoryRoleSources takes the sources' advisory locks in one fixed
// order, so two overlapping batches cannot deadlock.
func (s *Service) lockDirectoryRoleSources(ctx context.Context, dbtx pgx.Tx, lockKeys []string) error {
	sortedKeys := slices.Clone(lockKeys)
	slices.Sort(sortedKeys)
	queries := repo.New(dbtx)
	for _, lockKey := range sortedKeys {
		if err := queries.LockDirectoryRoleMappingSource(ctx, lockKey); err != nil {
			return oops.E(oops.CodeUnexpected, err, "lock directory role mapping source").LogError(ctx, s.logger)
		}
	}
	return nil
}

// directoryRoleSetupApplied is what applyDirectoryRoleSetupTx wrote.
type directoryRoleSetupApplied struct {
	createdRoles    []*gen.Role
	mappings        []*gen.DirectoryRoleMapping
	reconciliations []RoleReconciliation
}

// applyDirectoryRoleSetupTx checks each item's version, creates the requested
// roles and replaces each source's role set on dbtx. The caller holds every
// item's source lock, has checked the actor is a live organization admin, and
// commits, then calls reconcileDirectorySetupRoles.
func (s *Service) applyDirectoryRoleSetupTx(ctx context.Context, dbtx pgx.Tx, ac *contextvalues.AuthContext, workosOrgID string, items []directoryRoleSetupItem, grantSet []*gen.RoleGrant) (*directoryRoleSetupApplied, error) {
	orgID := ac.ActiveOrganizationID
	for _, planned := range items {
		version, err := s.directorySourceVersionTx(ctx, dbtx, orgID, planned.groupID, planned.item.Source.AttributeKey, planned.item.Source.AttributeValue)
		if err != nil {
			return nil, err
		}
		if version != planned.item.Version {
			return nil, oops.E(oops.CodeConflict, nil, "the directory or its role mappings changed since the preview; preview again").LogWarn(ctx, s.logger)
		}
	}

	if grantSet == nil {
		grantSet = []*gen.RoleGrant{}
	}
	actor := accessAuditActor{Principal: urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID), DisplayName: ac.Email}
	createdBySlug := map[string]*gen.Role{}
	createdRoles := []*gen.Role{}
	reconciliations := []RoleReconciliation{}
	for _, planned := range items {
		for _, role := range planned.item.Roles {
			if role.CreateName == nil {
				continue
			}
			slug, err := slugify(*role.CreateName)
			if err != nil {
				return nil, err
			}
			if _, ok := createdBySlug[slug]; ok {
				continue
			}
			created, reconciliation, err := s.roleMgr.CreateRoleTx(ctx, dbtx, orgID, workosOrgID, actor, &gen.CreateRolePayload{
				ApikeyToken: nil, SessionToken: nil,
				Name: *role.CreateName, Description: nil, Grants: grantSet, MemberIds: nil, AgentIds: nil,
			})
			if err != nil {
				return nil, err
			}
			createdBySlug[slug] = created.Role
			createdRoles = append(createdRoles, created.Role)
			reconciliations = append(reconciliations, reconciliation)
		}
	}

	mappings := []*gen.DirectoryRoleMapping{}
	for _, planned := range items {
		roleURNs := make([]string, 0, len(planned.item.Roles))
		for _, role := range planned.item.Roles {
			if role.RoleUrn != nil {
				roleURNs = append(roleURNs, *role.RoleUrn)
				continue
			}
			// Validated and created above.
			slug, _ := slugify(*role.CreateName)
			roleURNs = append(roleURNs, createdBySlug[slug].PrincipalUrn)
		}
		source := planned.item.Source
		written, err := s.writeDirectoryRoleSetTx(ctx, dbtx, ac, &gen.SetDirectoryRoleMappingsPayload{
			ApikeyToken: nil, SessionToken: nil,
			SourceKind: source.SourceKind, DirectoryGroupID: source.DirectoryGroupID,
			AttributeKey: source.AttributeKey, AttributeValue: source.AttributeValue,
			ExpectedRoleUrns: nil, RoleUrns: roleURNs,
		}, planned.groupID, false)
		if err != nil {
			return nil, err
		}
		mappings = append(mappings, written...)
	}

	return &directoryRoleSetupApplied{createdRoles: createdRoles, mappings: mappings, reconciliations: reconciliations}, nil
}

// reconcileDirectorySetupRoles tells WorkOS about new roles after commit, best
// effort, as for any role creation. Creating roles sets up no plugin
// distribution.
func (s *Service) reconcileDirectorySetupRoles(ctx context.Context, applied *directoryRoleSetupApplied) []*gen.DirectoryRoleSetupSideEffect {
	sideEffects := make([]*gen.DirectoryRoleSetupSideEffect, 0, len(applied.createdRoles))
	for i, reconciliation := range applied.reconciliations {
		s.roleMgr.ReconcileRole(ctx, reconciliation)
		sideEffects = append(sideEffects, &gen.DirectoryRoleSetupSideEffect{
			Kind: "workos_role_sync", RoleID: applied.createdRoles[i].ID, RequestStatus: "requested",
		})
	}
	return sideEffects
}

// directorySourceVersionTx recomputes a source's preview version on dbtx.
func (s *Service) directorySourceVersionTx(ctx context.Context, dbtx pgx.Tx, organizationID string, groupID uuid.NullUUID, attributeKey, attributeValue *string) (string, error) {
	queries := repo.New(dbtx)
	sourceKind := directoryRoleMappingSourceAttribute
	sourceID := conv.PtrValOr(attributeKey, "") + "\x00" + conv.PtrValOr(attributeValue, "")
	sourceName := conv.PtrValOr(attributeKey, "") + "=" + conv.PtrValOr(attributeValue, "")
	if groupID.Valid {
		group, err := queries.GetDirectoryRoleMappingGroupSource(ctx, repo.GetDirectoryRoleMappingGroupSourceParams{
			ID:             groupID.UUID,
			OrganizationID: organizationID,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return "", oops.E(oops.CodeNotFound, err, "directory group not found").LogWarn(ctx, s.logger)
		case err != nil:
			return "", oops.E(oops.CodeUnexpected, err, "get directory group").LogError(ctx, s.logger)
		}
		sourceKind, sourceID, sourceName = directoryRoleMappingSourceGroup, groupID.UUID.String(), group.Name
	}

	current, err := queries.ListLiveDirectoryRoleMappingsForSource(ctx, repo.ListLiveDirectoryRoleMappingsForSourceParams{
		OrganizationID:   organizationID,
		DirectoryGroupID: groupID,
		AttributeKey:     conv.PtrToPGText(attributeKey),
		AttributeValue:   conv.PtrToPGText(attributeValue),
	})
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "list current directory role mappings").LogError(ctx, s.logger)
	}
	roleURNs := make([]string, 0, len(current))
	for _, row := range current {
		roleURNs = append(roleURNs, row.RoleUrn)
	}
	return directorySourceVersion(sourceKind, sourceID, sourceName, sortedUnique(roleURNs)), nil
}
