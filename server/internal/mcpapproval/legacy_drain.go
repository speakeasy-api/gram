package mcpapproval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// bypassTargetKindShadowMCPServer is the legacy bypass-request target kind a
// shadow-MCP block writes. Mirrored here (as in the access service) rather
// than imported: the risk service package depends on far more than this one
// string.
const bypassTargetKindShadowMCPServer = "shadow_mcp_server"

// drainLegacyBypassRequests resolves the legacy bypass rows a decision
// covers, in the decision's own transaction.
//
// Bypass rows are per-requester: one blocked server can hold several pending
// asks from different people, and only one of them is ever linked to the
// review through a promotion. The decision on the review answers all of them,
// so every still-requested shadow-MCP row keyed to the same canonical URL is
// resolved — not just the linked one — plus the linked row itself when URL
// matching cannot place it. Rows already decided (or deleted) through the
// legacy queue keep their recorded outcome: the resolve query only touches
// rows still in the requested status.
//
// Each transition is audited with the same action, snapshots, and metadata
// the legacy approve/deny endpoints record, actor = the decider, so draining
// through a review decision leaves the same trail the legacy queue would
// have.
func (s *Service) drainLegacyBypassRequests(
	ctx context.Context,
	dbtx pgx.Tx,
	request repo.GetApprovalRequestForDecisionRow,
	projectID uuid.UUID,
	decision string,
	granted []string,
	authCtx *contextvalues.AuthContext,
) error {
	q := riskrepo.New(dbtx)

	candidates := make([]riskrepo.RiskPolicyBypassRequest, 0, 4)
	seen := make(map[uuid.UUID]struct{})

	if request.TargetKind == targetKindServerURL {
		rows, err := q.ListRiskPolicyBypassRequests(ctx, riskrepo.ListRiskPolicyBypassRequestsParams{
			ProjectID:    projectID,
			RiskPolicyID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			Status:       conv.ToPGText(statusRequested),
		})
		if err != nil {
			return fmt.Errorf("list pending bypass requests: %w", err)
		}
		for _, row := range rows {
			if conv.FromPGTextOrEmpty[string](row.TargetKind) != bypassTargetKindShadowMCPServer {
				continue
			}
			// request.TargetKey is the canonical inventory URL for server_url
			// targets — the same key blocks and the org's traffic converge on.
			if bypassRowCanonicalURL(row) != request.TargetKey {
				continue
			}
			candidates = append(candidates, row)
			seen[row.ID] = struct{}{}
		}
	}

	// The promotion source resolves even when URL matching cannot place it
	// (e.g. a row whose URL dimension no longer canonicalizes).
	if request.RiskPolicyBypassRequestID.Valid {
		if _, ok := seen[request.RiskPolicyBypassRequestID.UUID]; !ok {
			row, err := q.GetRiskPolicyBypassRequest(ctx, riskrepo.GetRiskPolicyBypassRequestParams{
				ID:        request.RiskPolicyBypassRequestID.UUID,
				ProjectID: projectID,
			})
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				// Deleted through the legacy drain; nothing left to resolve.
			case err != nil:
				return fmt.Errorf("read promoted bypass request: %w", err)
			default:
				candidates = append(candidates, row)
			}
		}
	}

	for _, before := range candidates {
		resolved, err := q.ResolveRequestedRiskPolicyBypassRequest(ctx, riskrepo.ResolveRequestedRiskPolicyBypassRequestParams{
			Status:               statusFor[decision],
			DecidedBy:            conv.ToPGText(authCtx.UserID),
			GrantedPrincipalUrns: granted,
			ID:                   before.ID,
			ProjectID:            projectID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			// Already decided through the legacy queue; its outcome stands.
			continue
		}
		if err != nil {
			return fmt.Errorf("resolve bypass request: %w", err)
		}
		if err := s.auditBypassRequestResolution(ctx, dbtx, projectID, decision, authCtx, before, resolved); err != nil {
			return fmt.Errorf("audit bypass request resolution: %w", err)
		}
	}

	return nil
}

// auditBypassRequestResolution records the drained row's status transition —
// before/after snapshots and metadata in the shape the legacy approve/deny
// endpoints write, so audit consumers see one consistent trail whichever
// surface resolved the ask.
func (s *Service) auditBypassRequestResolution(
	ctx context.Context,
	dbtx pgx.Tx,
	projectID uuid.UUID,
	decision string,
	authCtx *contextvalues.AuthContext,
	before riskrepo.RiskPolicyBypassRequest,
	after riskrepo.RiskPolicyBypassRequest,
) error {
	// Subject display carries the policy name the way the legacy endpoints
	// recorded it; a policy deleted since the ask still audits, just unnamed.
	policyName := ""
	policy, err := riskrepo.New(dbtx).GetRiskPolicy(ctx, riskrepo.GetRiskPolicyParams{
		ID:        after.RiskPolicyID,
		ProjectID: projectID,
	})
	switch {
	case err == nil:
		policyName = policy.Name
	case !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("read risk policy for audit: %w", err)
	}

	beforeSnapshot, err := bypassRequestAuditSnapshot(before)
	if err != nil {
		return fmt.Errorf("snapshot bypass request before resolution: %w", err)
	}
	afterSnapshot, err := bypassRequestAuditSnapshot(after)
	if err != nil {
		return fmt.Errorf("snapshot bypass request after resolution: %w", err)
	}
	dimensions, err := bypassRowDimensions(after.TargetDimensions)
	if err != nil {
		return fmt.Errorf("decode bypass request dimensions: %w", err)
	}

	event := audit.LogRiskPolicyBypassRequestEvent{
		OrganizationID:                    after.OrganizationID,
		ProjectID:                         projectID,
		Actor:                             urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:                  authCtx.Email,
		ActorSlug:                         nil,
		RiskPolicyID:                      after.RiskPolicyID,
		RiskPolicyName:                    policyName,
		PolicyBypassRequestSnapshotBefore: beforeSnapshot,
		PolicyBypassRequestSnapshotAfter:  afterSnapshot,
		Metadata: &audit.RiskPolicyBypassRequestMetadata{
			RequestID:            after.ID.String(),
			TargetKind:           conv.FromPGTextOrEmpty[string](after.TargetKind),
			TargetKey:            conv.FromPGTextOrEmpty[string](after.TargetKey),
			TargetDimensions:     dimensions,
			RequesterUserID:      after.RequesterUserID,
			GrantedPrincipalURNs: slices.Clone(after.GrantedPrincipalUrns),
			PreviousStatus:       before.Status,
			CurrentStatus:        after.Status,
		},
	}

	if decision == decisionApproved {
		if err := s.audit.LogRiskPolicyBypassRequestApprove(ctx, dbtx, event); err != nil {
			return fmt.Errorf("log bypass request approve: %w", err)
		}
		return nil
	}
	if err := s.audit.LogRiskPolicyBypassRequestDeny(ctx, dbtx, event); err != nil {
		return fmt.Errorf("log bypass request deny: %w", err)
	}
	return nil
}

// bypassRowCanonicalURL resolves the canonical inventory URL a legacy bypass
// row targets, or "" when it has none (identity-only rows).
func bypassRowCanonicalURL(row riskrepo.RiskPolicyBypassRequest) string {
	if dimensions, err := bypassRowDimensions(row.TargetDimensions); err == nil {
		if raw := strings.TrimSpace(dimensions[authz.SelectorKeyServerURL]); raw != "" {
			if inventoryURL, ok := shadowmcp.CanonicalizeInventoryURL(raw); ok {
				return inventoryURL.CanonicalURL
			}
			return ""
		}
	}
	if raw := strings.TrimSpace(conv.FromPGTextOrEmpty[string](row.TargetKey)); raw != "" {
		if inventoryURL, ok := shadowmcp.CanonicalizeInventoryURL(raw); ok {
			return inventoryURL.CanonicalURL
		}
	}
	return ""
}

func bypassRequestAuditSnapshot(row riskrepo.RiskPolicyBypassRequest) (*audit.RiskPolicyBypassRequestSnapshot, error) {
	dimensions, err := bypassRowDimensions(row.TargetDimensions)
	if err != nil {
		return nil, err
	}

	decidedAt := conv.FromPGTimestamptz(row.DecidedAt)
	var decidedAtPtr *string
	if decidedAt != "" {
		decidedAtPtr = &decidedAt
	}

	return &audit.RiskPolicyBypassRequestSnapshot{
		ID:                   row.ID.String(),
		PolicyID:             row.RiskPolicyID.String(),
		TargetKind:           conv.FromPGText[string](row.TargetKind),
		TargetLabel:          conv.FromPGText[string](row.TargetLabel),
		TargetKey:            conv.FromPGText[string](row.TargetKey),
		TargetDimensions:     dimensions,
		RequesterUserID:      row.RequesterUserID,
		RequesterEmail:       conv.FromPGText[string](row.RequesterEmail),
		Note:                 conv.FromPGText[string](row.Note),
		Status:               row.Status,
		DecidedBy:            conv.FromPGText[string](row.DecidedBy),
		GrantedPrincipalURNs: slices.Clone(row.GrantedPrincipalUrns),
		DecidedAt:            decidedAtPtr,
		CreatedAt:            conv.FromPGTimestamptz(row.CreatedAt),
		UpdatedAt:            conv.FromPGTimestamptz(row.UpdatedAt),
	}, nil
}

func bypassRowDimensions(raw []byte) (map[string]string, error) {
	if len(raw) == 0 {
		return map[string]string{}, nil
	}

	var dimensions map[string]string
	if err := json.Unmarshal(raw, &dimensions); err != nil {
		return nil, fmt.Errorf("unmarshal dimensions: %w", err)
	}
	if dimensions == nil {
		return map[string]string{}, nil
	}

	return dimensions, nil
}
