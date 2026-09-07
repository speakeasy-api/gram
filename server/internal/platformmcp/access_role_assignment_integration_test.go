package platformmcp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/access"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func TestAssignMCPAccessRolePreservesRolesAndReplays(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_access_role_assignment")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	organization, err := organizationsrepo.New(conn).GetOrganizationMetadata(ctx, principal.OrganizationID)
	require.NoError(t, err)
	_, err = organizationsrepo.New(conn).UpsertOrganizationMetadata(ctx, organizationsrepo.UpsertOrganizationMetadataParams{
		ID: principal.OrganizationID, Name: organization.Name, Slug: organization.Slug,
		WorkosID: conv.ToPGText("workos-" + uuid.NewString()), Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	for i := range 5 {
		seedAccessMember(t, ctx, conn, principal.OrganizationID, "assignment-member-"+string(rune('a'+i)), "member"+string(rune('a'+i))+"@example.test")
	}
	memberID := "assignment-member-a"
	baseRole := seedAccessRole(t, ctx, conn, principal.OrganizationID, "org-base-role", "Base Role")
	targetRole := seedAccessRole(t, ctx, conn, principal.OrganizationID, "org-mcp-operators", "MCP Operators")
	_, err = accessrepo.New(conn).UpsertOrganizationRoleAssignment(ctx, accessrepo.UpsertOrganizationRoleAssignmentParams{
		OrganizationID: principal.OrganizationID, WorkosUserID: "workos-" + memberID, WorkosRoleSlug: "org-base-role",
		UserID: conv.ToPGText(memberID), WorkosMembershipID: conv.ToPGText("membership-" + memberID), WorkosUpdatedAt: conv.ToPGTimestamptz(organization.UpdatedAt.Time), WorkosLastEventID: pgtype.Text{},
	})
	require.NoError(t, err)

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagPlatformMCPAccessRoleMutations, principal.OrganizationID, true)
	logger := testenv.NewLogger(t)
	// A single connection catches any pool read attempted inside the receipt
	// transaction: the assignment must complete using only that transaction.
	config := conn.Config()
	config.MaxConns = 1
	config.MinConns = 0
	single, err := pgxpool.NewWithConfig(ctx, config)
	require.NoError(t, err)
	t.Cleanup(single.Close)
	reads := NewAccessReadService(logger, single, allowBudget(), "assignment-integration-key")
	manager := access.NewRoleManager(logger, single, workos.NewStubClient(), audit.NewLogger())
	roles, err := NewAccessRoleMutationService(reads, flags, allowBudget(), "assignment-integration-key", manager)
	require.NoError(t, err)
	service, err := NewAccessRoleAssignmentService(roles)
	require.NoError(t, err)

	members, err := reads.ListMembers(ctx, principal, ListAccessMembersInput{Query: "member", Limit: 10})
	require.NoError(t, err)
	require.False(t, members.Suppressed)
	var member AccessMember
	for _, candidate := range members.Members {
		resolved, decodeErr := reads.references.Decode(candidate.Reference, principal, subjectKindAccessMember, reads.now())
		if decodeErr == nil && resolved == memberID {
			member = candidate
		}
	}
	require.NotEmpty(t, member.Reference)
	require.NotEmpty(t, member.Version)
	roleReference, err := reads.references.Encode(principal, subjectKindAccessRole, strings.TrimPrefix(targetRole.ID, "organization:"), reads.now())
	require.NoError(t, err)

	beforeAudit, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionAccessMemberRoleUpdate)
	require.NoError(t, err)
	input := AssignMCPAccessRoleInput{ProjectID: project.ID.String(), MemberReference: member.Reference, RoleReference: roleReference, ExpectedVersion: member.Version, IdempotencyKey: "assign-member-role", Confirmed: true}
	assignCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	first, err := service.Assign(assignCtx, principal, input)
	require.NoError(t, err)
	require.Equal(t, "assigned", first.ResultCategory)
	require.ElementsMatch(t, []string{"Base Role", "MCP Operators"}, first.Member.Roles)
	require.False(t, first.Receipt.Replayed)
	require.Equal(t, "assignment_commit", first.SnapshotScope)

	replayed, err := service.Assign(ctx, principal, input)
	require.NoError(t, err)
	require.True(t, replayed.Receipt.Replayed)
	afterAudit, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionAccessMemberRoleUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeAudit+1, afterAudit)

	resolved, err := authz.ResolveUserPrincipals(ctx, conn, principal.OrganizationID, memberID)
	require.NoError(t, err)
	require.Contains(t, resolved, baseRole)
	require.Contains(t, resolved, targetRole)

	currentMembers, err := reads.ListMembers(ctx, principal, ListAccessMembersInput{Query: "member", Limit: 10})
	require.NoError(t, err)
	var current AccessMember
	for _, candidate := range currentMembers.Members {
		resolvedID, decodeErr := reads.references.Decode(candidate.Reference, principal, subjectKindAccessMember, reads.now())
		if decodeErr == nil && resolvedID == memberID {
			current = candidate
		}
	}
	already, err := service.Assign(ctx, principal, AssignMCPAccessRoleInput{ProjectID: project.ID.String(), MemberReference: current.Reference, RoleReference: roleReference, ExpectedVersion: current.Version, IdempotencyKey: "already-assigned-member-role", Confirmed: true})
	require.NoError(t, err)
	require.Equal(t, "already_assigned", already.ResultCategory)
	afterAlreadyAudit, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionAccessMemberRoleUpdate)
	require.NoError(t, err)
	require.Equal(t, afterAudit, afterAlreadyAudit)

	_, err = service.Assign(ctx, principal, AssignMCPAccessRoleInput{ProjectID: project.ID.String(), MemberReference: member.Reference, RoleReference: roleReference, ExpectedVersion: member.Version, IdempotencyKey: "stale-member-role", Confirmed: true})
	require.ErrorIs(t, err, ErrAccessRoleMutationConflict)
	_, err = platformrepo.New(conn).GetPlatformMCPOperationReceipt(ctx, platformrepo.GetPlatformMCPOperationReceiptParams{OrganizationID: principal.OrganizationID, ProjectID: project.ID, Operation: operationAssignMCPAccessRole, IdempotencyKey: "stale-member-role", UserID: conv.ToPGText(principal.UserID), SubjectUrn: userSubjectURN(principal.UserID)})
	require.Error(t, err)

	// System roles are rejected before assignment or audit, even when the
	// system slug lives in the organization role table.
	adminRole := seedAccessRole(t, ctx, conn, principal.OrganizationID, "admin", "Admin")
	adminReference, err := reads.references.Encode(principal, subjectKindAccessRole, strings.TrimPrefix(adminRole.ID, "organization:"), reads.now())
	require.NoError(t, err)
	_, err = service.Assign(ctx, principal, AssignMCPAccessRoleInput{ProjectID: project.ID.String(), MemberReference: current.Reference, RoleReference: adminReference, ExpectedVersion: current.Version, IdempotencyKey: "reject-system-role", Confirmed: true})
	require.ErrorIs(t, err, ErrAccessRoleMutationNotFound)
	resolved, err = authz.ResolveUserPrincipals(ctx, conn, principal.OrganizationID, memberID)
	require.NoError(t, err)
	require.NotContains(t, resolved, adminRole)
	finalAudit, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionAccessMemberRoleUpdate)
	require.NoError(t, err)
	require.Equal(t, afterAudit, finalAudit)

	// A receipt remains historical after later removals; replay cannot restore
	// the assignment, and the snapshot version is not a new-write version.
	_, err = accessrepo.New(conn).SoftDeleteAllRoleAssignmentsByWorkosUser(ctx, accessrepo.SoftDeleteAllRoleAssignmentsByWorkosUserParams{OrganizationID: principal.OrganizationID, WorkosUserID: "workos-" + memberID})
	require.NoError(t, err)
	replayed, err = service.Assign(ctx, principal, input)
	require.NoError(t, err)
	require.True(t, replayed.Receipt.Replayed)
	require.Equal(t, "assignment_commit", replayed.SnapshotScope)
	require.Equal(t, first.Member, replayed.Member)
	active, err := accessrepo.New(conn).ListActiveRoleIDsByWorkosUser(ctx, accessrepo.ListActiveRoleIDsByWorkosUserParams{OrganizationID: principal.OrganizationID, WorkosUserID: "workos-" + memberID})
	require.NoError(t, err)
	require.Empty(t, active)

	require.NoError(t, usersrepo.New(conn).OverwriteUserWorkosID(ctx, usersrepo.OverwriteUserWorkosIDParams{ID: memberID, WorkosID: pgtype.Text{}}))
	replayed, err = service.Assign(ctx, principal, input)
	require.NoError(t, err)
	require.True(t, replayed.Receipt.Replayed)
	require.Equal(t, "not_applicable", replayed.Reconciliation)

	require.NoError(t, organizationsrepo.New(conn).DeleteOrganizationUserRelationship(ctx, organizationsrepo.DeleteOrganizationUserRelationshipParams{OrganizationID: principal.OrganizationID, UserID: conv.ToPGText(memberID)}))
	replayed, err = service.Assign(ctx, principal, input)
	require.NoError(t, err)
	require.True(t, replayed.Receipt.Replayed)
	require.Equal(t, first.Receipt.ID, replayed.Receipt.ID)
	require.Equal(t, "not_applicable", replayed.Reconciliation)
}
