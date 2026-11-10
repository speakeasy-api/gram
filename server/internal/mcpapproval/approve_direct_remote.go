package mcpapproval

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/identity"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	shadowadmission "github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var _ shadowadmission.Approver = (*Service)(nil)

// ApproveDirectRemote records an organization administrator's exposure write as
// the standing approval it stands for. It converges on the server's one
// approval request (creating it if the inventory has never seen the URL),
// attaches the administrator as its requester, and records an approved
// decision whose audience is the union of what was already approved and the
// audience this write reaches. The whole thing shares the caller's transaction
// with the write itself.
//
// No evidence is gathered here: that does network I/O, which must not run
// under the caller's locks, and the administrator has already decided. The
// request is left for the usual evidence refresh to fill in.
func (s *Service) ApproveDirectRemote(ctx context.Context, tx pgx.Tx, input shadowadmission.ApproveDirectRemoteInput) error {
	if s == nil || s.db == nil || s.audit == nil || tx == nil || input.OrganizationID == "" || input.ProjectID == uuid.Nil || input.ActorUserID == "" || strings.TrimSpace(input.CanonicalURL) == "" {
		return oops.E(oops.CodeBadRequest, nil, "invalid administrator approval")
	}
	key, display, err := admittableSecureServerURL(input.CanonicalURL)
	if err != nil {
		return fmt.Errorf("canonicalize administrator-approved server: %w", err)
	}
	rationale := strings.TrimSpace(input.Rationale)
	if rationale == "" {
		rationale = "Approved by an organization administrator who shared this server."
	}

	resolved := identity.Resolve(display)
	queries := repo.New(s.db).WithTx(tx)
	request, err := queries.UpsertApprovalRequest(ctx, repo.UpsertApprovalRequestParams{
		OrganizationID:            input.OrganizationID,
		ProjectID:                 input.ProjectID,
		TargetKind:                targetKindServerURL,
		TargetRaw:                 display,
		TargetKey:                 key,
		ArtifactRef:               conv.ToPGTextEmpty(resolved.ArtifactRef),
		VersionPinned:             resolved.VersionPinned,
		Status:                    statusRequested,
		RiskPolicyBypassRequestID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	if err != nil {
		return fmt.Errorf("record administrator-approved server: %w", err)
	}
	if _, err := queries.UpsertApprovalRequestRequester(ctx, repo.UpsertApprovalRequestRequesterParams{
		OrganizationID:       input.OrganizationID,
		ProjectID:            input.ProjectID,
		McpApprovalRequestID: request.ID,
		UserID:               input.ActorUserID,
		UserEmail:            conv.PtrToPGTextEmpty(input.ActorEmail),
		Note:                 conv.ToPGTextEmpty(rationale),
	}); err != nil {
		return fmt.Errorf("record administrator as requester: %w", err)
	}
	if request.Inserted {
		if err := s.audit.LogMCPApprovalRequestCreate(ctx, tx, audit.LogMCPApprovalRequestCreateEvent{
			OrganizationID:   input.OrganizationID,
			ProjectID:        input.ProjectID,
			Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, input.ActorUserID),
			ActorDisplayName: input.ActorEmail,
			ActorSlug:        nil,
			RequestURN:       urn.NewMCPApprovalRequest(request.ID),
			TargetRaw:        display,
		}); err != nil {
			return fmt.Errorf("audit administrator-approved server: %w", err)
		}
	}

	// Widen, never narrow: a standing approval for other people stays in force
	// alongside the audience this write reaches.
	granted := make([]string, 0, len(input.PrincipalURNs)+4)
	standing, err := queries.GetStandingServerDecisionForAdmission(ctx, repo.GetStandingServerDecisionForAdmissionParams{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		TargetKey:      key,
	})
	switch {
	case err == nil && standing.Decision == decisionApproved:
		// A principal approved earlier may since have left the organization or
		// been deleted; the decision write would reject it as new input, which
		// must not block an unrelated administrator's write. Carry forward
		// only what still resolves.
		for _, principalURN := range standing.GrantedPrincipalUrns {
			resolves, err := principalResolves(ctx, tx, input.OrganizationID, principalURN)
			if err != nil {
				return err
			}
			if resolves {
				granted = append(granted, principalURN)
			}
		}
	case err != nil && !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("read standing decision for administrator approval: %w", err)
	}
	for _, principalURN := range input.PrincipalURNs {
		if principalURN == urn.PrincipalWildcard {
			principalURN = authz.AllUsersPrincipal().String()
		}
		granted = append(granted, principalURN)
	}
	granted = conv.DedupeNonEmpty(granted)
	if slices.Contains(granted, authz.AllUsersPrincipal().String()) {
		// Everyone subsumes every narrower audience.
		granted = []string{authz.AllUsersPrincipal().String()}
	}

	if _, err := s.DecideInTransaction(ctx, tx, DecisionCommandInput{
		OrganizationID:       input.OrganizationID,
		ProjectID:            input.ProjectID,
		RequestID:            request.ID,
		Decision:             decisionApproved,
		Rationale:            rationale,
		GrantedPrincipalURNs: granted,
		ResearchReportID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ActorUserID:          input.ActorUserID,
		ActorEmail:           input.ActorEmail,
		ValidateLocked:       nil,
	}); err != nil {
		return fmt.Errorf("record administrator decision: %w", err)
	}
	return nil
}

// principalResolves reports whether a previously approved principal still
// exists in the organization. A malformed or vanished principal is simply no
// longer part of the audience; any other failure is returned.
func principalResolves(ctx context.Context, tx pgx.Tx, organizationID, principalURN string) (bool, error) {
	principal, err := urn.ParsePrincipal(principalURN)
	if err != nil {
		return false, nil //nolint:nilerr // an unparseable stored principal is dropped, not fatal
	}
	if err := authz.ValidatePrincipal(ctx, tx, organizationID, principal); err != nil {
		if errors.Is(err, authz.ErrPrincipalInvalid) || errors.Is(err, authz.ErrPrincipalNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("validate previously approved principal: %w", err)
	}
	return true, nil
}
