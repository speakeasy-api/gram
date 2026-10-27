package approval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/risk/approval/repo"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// ListInput selects a page of a project's approval requests.
type ListInput struct {
	// OrganizationID is the organization the project belongs to.
	OrganizationID string

	// ProjectID is the project to list.
	ProjectID uuid.UUID

	// Status filters on the status a viewer sees. Empty lists every status.
	Status Status

	// VisiblePolicyIDs narrows the list to these policies plus the requests
	// ViewerUserID made. Nil lists every request in the project.
	VisiblePolicyIDs []uuid.UUID

	// ViewerUserID is the person listing.
	ViewerUserID string

	// Cursor continues after this request id. Nil starts at the newest.
	Cursor uuid.NullUUID

	// Limit bounds the page size.
	Limit int32
}

// List returns a page of approval requests, newest first.
func (s *Service) List(ctx context.Context, input ListInput) ([]repo.RiskPolicyApproval, error) {
	rows, err := repo.New(s.db).ListApprovals(ctx, repo.ListApprovalsParams{
		OrganizationID:   input.OrganizationID,
		ProjectID:        input.ProjectID,
		Status:           conv.ToPGTextEmpty(string(input.Status)),
		VisiblePolicyIds: input.VisiblePolicyIDs,
		ViewerUserID:     conv.ToPGTextEmpty(input.ViewerUserID),
		Cursor:           input.Cursor,
		PageLimit:        input.Limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list approvals: %w", err)
	}
	return rows, nil
}

// Get returns one approval request in a project.
func (s *Service) Get(ctx context.Context, organizationID string, projectID, approvalID uuid.UUID) (repo.RiskPolicyApproval, error) {
	row, err := repo.New(s.db).GetApproval(ctx, repo.GetApprovalParams{OrganizationID: organizationID, ProjectID: projectID, ID: approvalID})
	if errors.Is(err, pgx.ErrNoRows) {
		return repo.RiskPolicyApproval{}, ErrNotFound
	}
	if err != nil {
		return repo.RiskPolicyApproval{}, fmt.Errorf("get approval: %w", err)
	}
	return row, nil
}

// ScopeKind reads the kind out of a stored requested or granted scope. It is
// empty when no scope is stored.
func ScopeKind(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var doc scopeDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return ""
	}
	return doc.Kind
}

// policyName names the policy behind a request. A policy deleted since the
// call was held no longer requires approval, so its requests are not found.
func (s *Service) policyName(ctx context.Context, db riskrepo.DBTX, row repo.RiskPolicyApproval) (string, error) {
	policy, err := riskrepo.New(db).GetRiskPolicy(ctx, riskrepo.GetRiskPolicyParams{ID: row.RiskPolicyID, ProjectID: row.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && policy.OrganizationID != row.OrganizationID) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get policy: %w", err)
	}
	return policy.Name, nil
}

// auditSnapshot is the audited state of a request, without its token or the
// call's arguments.
func auditSnapshot(row repo.RiskPolicyApproval) *audit.RiskPolicyApprovalSnapshot {
	return &audit.RiskPolicyApprovalSnapshot{
		ID:             row.ID.String(),
		PolicyID:       row.RiskPolicyID.String(),
		PolicyVersion:  row.PolicyVersion,
		Status:         row.Status,
		RequesterURN:   row.RequesterUrn,
		Surface:        row.Surface,
		ToolName:       row.ToolName,
		Reason:         conv.FromPGText[string](row.Reason),
		RequestedScope: optionalString(ScopeKind(row.RequestedScope)),
		GrantedScope:   optionalString(ScopeKind(row.GrantedScope)),
		DecidedByURN:   conv.FromPGText[string](row.DecidedByUrn),
		DecidedVia:     conv.FromPGText[string](row.DecidedVia),
		DecisionNote:   conv.FromPGText[string](row.DecisionNote),
		RequestedAt:    formatTimestamp(row.RequestedAt),
		DecidedAt:      formatTimestamp(row.DecidedAt),
		ExpiresAt:      formatTimestamp(row.ExpiresAt),
	}
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func formatTimestamp(value pgtype.Timestamptz) *string {
	if !value.Valid {
		return nil
	}
	formatted := value.Time.UTC().Format(time.RFC3339)
	return &formatted
}
