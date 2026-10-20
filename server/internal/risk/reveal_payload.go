package risk

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// RevealRiskResultPayload returns the full scanned payload of the MCP execution
// phase a finding was raised on. The payload can hold content from any chat or
// none, so it requires an unrestricted chat:read grant. Successful reveals are
// audited.
func (s *Service) RevealRiskResultPayload(ctx context.Context, payload *gen.RevealRiskResultPayloadPayload) (*gen.RiskRevealPayloadResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	projectID := *authCtx.ProjectID

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.C(oops.CodeInvalid)
	}

	row, err := s.findingsCH.GetRiskFindingForUnmask(ctx, chrepo.GetRiskFindingForUnmaskParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      projectID.String(),
		ID:             id,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load risk finding").LogError(ctx, s.logger)
	}
	if row == nil {
		return nil, oops.E(oops.CodeNotFound, nil, "risk result not found")
	}

	policyID, err := uuid.Parse(row.RiskPolicyID)
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "risk result not found")
	}
	visible, err := s.visiblePolicyIDs(ctx, projectID, uuid.NullUUID{UUID: policyID, Valid: true})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load risk policy").LogError(ctx, s.logger)
	}
	if len(visible) == 0 {
		return nil, oops.E(oops.CodeNotFound, nil, "risk result not found")
	}

	if row.MediationSurface == "" {
		return nil, oops.E(oops.CodeNotFound, nil, "risk result has no revealable MCP payload")
	}

	if err := s.authz.Require(ctx, authz.ChatReadCheck(uuid.Nil.String())); err != nil {
		return nil, err
	}

	notStored := &gen.RiskRevealPayloadResult{
		ID:          row.ID.String(),
		RevealState: riskUnmaskRevealStateEvidenceNotStored,
		ExecutionID: conv.PtrEmpty(row.ExecutionID),
		Phase:       conv.PtrEmpty(row.Phase),
		Payload:     "",
		ExpiresAt:   nil,
	}
	if s.findingEvidence == nil || row.ExecutionID == "" || row.Phase == "" {
		return notStored, nil
	}
	revealed, err := s.findingEvidence.RevealExecutionPayload(ctx, authCtx.ActiveOrganizationID, projectID, row.ExecutionID, row.Phase, time.Now().UTC())
	if errors.Is(err, ErrMCPFindingEvidenceNotStored) {
		return notStored, nil
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load MCP execution evidence").LogError(ctx, s.logger)
	}

	if err := s.audit.LogRiskResultRevealPayload(ctx, s.db, audit.LogRiskResultRevealPayloadEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        projectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		RiskResultID:     row.ID,
		ExecutionID:      row.ExecutionID,
		Phase:            row.Phase,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "record risk result payload reveal audit log").LogError(ctx, s.logger)
	}

	return &gen.RiskRevealPayloadResult{
		ID:          row.ID.String(),
		RevealState: riskUnmaskRevealStateAvailable,
		ExecutionID: conv.PtrEmpty(row.ExecutionID),
		Phase:       conv.PtrEmpty(row.Phase),
		Payload:     revealed.Payload,
		ExpiresAt:   new(revealed.ExpiresAt.UTC().Format(time.RFC3339)),
	}, nil
}
