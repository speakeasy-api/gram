package platformmcp

import (
	"context"
	"crypto/hmac"
	"errors"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type AssignMCPAccessRoleInput struct {
	ProjectID       string `json:"project_id" jsonschema:"explicit project ID for the access workflow"`
	MemberReference string `json:"member_reference" jsonschema:"opaque member reference returned by list_access_members"`
	RoleReference   string `json:"role_reference" jsonschema:"opaque custom role reference returned by list_access_roles or get_mcp_access"`
	ExpectedVersion string `json:"expected_version" jsonschema:"opaque member role version returned by list_access_members immediately before this write"`
	IdempotencyKey  string `json:"idempotency_key" jsonschema:"stable unique key for safely retrying this exact assignment"`
	Confirmed       bool   `json:"confirmed" jsonschema:"set true only after the user confirms the exact project, masked member, custom role, and effective MCP access"`
}

type AccessRoleAssignmentMember struct {
	MaskedIdentity string   `json:"masked_identity"`
	Roles          []string `json:"roles"`
	Version        string   `json:"version"`
}

type AssignMCPAccessRoleOutput struct {
	Member         AccessRoleAssignmentMember `json:"member" jsonschema:"historical member snapshot at assignment commit; reread list_access_members before using a version for another write"`
	SnapshotScope  string                     `json:"snapshot_scope" jsonschema:"assignment_commit: this response describes the committed operation, not current access"`
	AssignedRole   string                     `json:"assigned_role"`
	ResultCategory string                     `json:"result_category"`
	Reconciliation string                     `json:"reconciliation"`
	Receipt        RiskMutationToolReceipt    `json:"receipt"`
}

type normalizedAccessRoleAssignment struct {
	ProjectID       string `json:"project_id"`
	MemberID        string `json:"member_id"`
	RoleID          string `json:"role_id"`
	ExpectedVersion string `json:"expected_version"`
}

type AccessRoleAssignmentService struct {
	roles    *AccessRoleMutationService
	receipts *AccessRoleAssignmentReceiptStore
}

func NewAccessRoleAssignmentService(roles *AccessRoleMutationService) (*AccessRoleAssignmentService, error) {
	if roles == nil || !roles.valid() {
		return nil, ErrAccessRoleMutationUnavailable
	}
	return &AccessRoleAssignmentService{roles: roles, receipts: NewAccessRoleAssignmentReceiptStore(roles.reads.db)}, nil
}

func (s *AccessRoleAssignmentService) valid() bool {
	return s != nil && s.roles != nil && s.roles.valid() && s.receipts != nil
}

func (s *AccessRoleAssignmentService) Assign(ctx context.Context, principal Principal, input AssignMCPAccessRoleInput) (AssignMCPAccessRoleOutput, error) {
	if !input.Confirmed {
		return AssignMCPAccessRoleOutput{}, accessRoleMutationConfirmationRequired()
	}
	if !s.valid() {
		return AssignMCPAccessRoleOutput{}, accessRoleMutationUnavailable(nil)
	}
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.MemberReference = strings.TrimSpace(input.MemberReference)
	input.RoleReference = strings.TrimSpace(input.RoleReference)
	input.ExpectedVersion = strings.TrimSpace(input.ExpectedVersion)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.MemberReference == "" || input.RoleReference == "" || !validAccessRoleVersion(input.ExpectedVersion) || input.IdempotencyKey == "" || len(input.IdempotencyKey) > 128 {
		return AssignMCPAccessRoleOutput{}, accessRoleMutationInvalid("The access role assignment request is invalid.")
	}
	project, _, err := s.roles.admit(ctx, principal, input.ProjectID)
	if err != nil {
		return AssignMCPAccessRoleOutput{}, err
	}
	memberID, err := s.roles.reads.references.Decode(input.MemberReference, principal, subjectKindAccessMember, s.roles.reads.now())
	if err != nil {
		return AssignMCPAccessRoleOutput{}, accessRoleMutationNotFound()
	}
	roleID, err := s.roles.reads.references.Decode(input.RoleReference, principal, subjectKindAccessRole, s.roles.reads.now())
	if err != nil {
		return AssignMCPAccessRoleOutput{}, accessRoleMutationNotFound()
	}
	normalized := normalizedAccessRoleAssignment{ProjectID: project.ID.String(), MemberID: memberID, RoleID: roleID, ExpectedVersion: input.ExpectedVersion}
	var reconciliation access.MemberRoleReconciliation
	receipt, err := s.receipts.Execute(ctx, principal, project, input.IdempotencyKey, normalized, func(ctx context.Context, tx pgx.Tx) (AccessRoleAssignmentReceiptResult, error) {
		result, pending, err := s.roles.backend.AddMemberRoleTx(ctx, tx, principal.OrganizationID, memberID, roleID, access.RoleAuditActor{
			Principal: urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID), DisplayName: nil,
		}, func(state access.MemberRoleState) error {
			actual, err := accessMemberRoleVersion(s.roles.versionKey, state.MemberID, state.RoleIDs)
			if err != nil || !hmac.Equal([]byte(actual), []byte(input.ExpectedVersion)) {
				return accessRoleMutationConflict("The member's roles changed after they were read. Read the member again and retry with the new version.")
			}
			return nil
		})
		if err != nil {
			return AccessRoleAssignmentReceiptResult{}, classifyAccessRoleBackendError(err)
		}
		reconciliation = pending
		return s.receiptResult(ctx, tx, principal, result, roleID)
	})
	if err != nil {
		return AssignMCPAccessRoleOutput{}, err
	}
	stored, err := decodeAccessRoleAssignmentReceipt(receipt.ResultPayload)
	if err != nil {
		return AssignMCPAccessRoleOutput{}, err
	}
	if receipt.Replayed {
		// Re-read current local desired state without repeating the assignment or
		// audit. A later dashboard removal must not be resurrected by an old replay.
		tx, beginErr := s.roles.reads.db.Begin(ctx)
		if beginErr == nil {
			reconciliation, beginErr = s.roles.backend.CurrentMemberRoleReconciliationTx(ctx, tx, principal.OrganizationID, memberID)
			_ = tx.Rollback(ctx)
		}
		if beginErr != nil {
			var shareable *oops.ShareableError
			if !errors.As(beginErr, &shareable) || shareable.Code != oops.CodeNotFound {
				return AssignMCPAccessRoleOutput{}, accessRoleMutationUnavailable(beginErr)
			}
			// The operation remains committed even if its member has since left
			// or lost provider linkage. There is no current state to reconcile.
			reconciliation = access.MemberRoleReconciliation{}
			stored.Reconciliation = "not_applicable"
		}
	}
	s.roles.backend.ReconcileMemberRoles(ctx, reconciliation)
	return AssignMCPAccessRoleOutput{
		Member:        AccessRoleAssignmentMember{MaskedIdentity: stored.MaskedIdentity, Roles: slices.Clone(stored.Roles), Version: stored.Version},
		SnapshotScope: "assignment_commit",
		AssignedRole:  stored.AssignedRole, ResultCategory: stored.ResultCategory, Reconciliation: stored.Reconciliation,
		Receipt: riskMutationToolReceipt(receipt),
	}, nil
}

func (s *AccessRoleAssignmentService) receiptResult(ctx context.Context, tx pgx.Tx, principal Principal, result access.MemberRoleAddResult, roleID string) (AccessRoleAssignmentReceiptResult, error) {
	assignedRole := ""
	names := make([]string, 0, len(result.After.RoleIDs))
	for _, id := range result.After.RoleIDs {
		current, err := s.roles.backend.GetRoleByIDTx(ctx, tx, principal.OrganizationID, id)
		if err != nil {
			return AccessRoleAssignmentReceiptResult{}, classifyAccessRoleBackendError(err)
		}
		if current == nil || strings.TrimSpace(current.Name) == "" {
			return AccessRoleAssignmentReceiptResult{}, accessRoleMutationUnavailable(errors.New("member role name is unavailable"))
		}
		names = append(names, current.Name)
		if id == roleID {
			if current.IsSystem {
				return AccessRoleAssignmentReceiptResult{}, accessRoleMutationNotFound()
			}
			assignedRole = current.Name
		}
	}
	if assignedRole == "" {
		return AccessRoleAssignmentReceiptResult{}, accessRoleMutationNotFound()
	}
	slices.Sort(names)
	version, err := accessMemberRoleVersion(s.roles.versionKey, result.After.MemberID, result.After.RoleIDs)
	if err != nil {
		return AccessRoleAssignmentReceiptResult{}, accessRoleMutationUnavailable(err)
	}
	masked := "Selected member"
	if result.Member != nil {
		masked = maskSubject(conv.Default(result.Member.Email, result.Member.Name))
	}
	category := "already_assigned"
	if result.Changed {
		category = "assigned"
	}
	return AccessRoleAssignmentReceiptResult{MaskedIdentity: masked, Roles: names, Version: version, AssignedRole: assignedRole, ResultCategory: category, Reconciliation: "pending"}, nil
}
