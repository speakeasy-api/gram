package agent

import (
	"context"
	"math"
	"slices"
	"strings"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

// ReportAIScan ingests one device-agent AI scan report: the matches land in
// the ai_detections inventory and the scan itself lands as a receipt, so a
// zero-match scan is still provable. The reporting identity mirrors
// GetPlugins (per-user key attributes to the key owner; the org install key
// requires a vouched email).
//
// Target ids are stored as reported: the agent's scan target list is
// compiled into its binary, so an updated agent can legitimately report ids
// this server's aitargets catalog does not know yet. For ids the catalog
// does know, the catalog's category wins over the reported one so the stored
// category filter stays consistent; unknown ids keep the reported category.
func (s *Service) ReportAIScan(ctx context.Context, payload *gen.ReportAIScanPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	// Resolve the reporting identity by credential type, mirroring GetPlugins.
	isInstallKey := slices.Contains(authCtx.APIKeyScopes, auth.APIKeyScopeAgent.String())
	var email string
	if isInstallKey {
		email = conv.NormalizeEmail(conv.PtrValOr(payload.Email, ""))
		if email == "" {
			return oops.E(oops.CodeBadRequest, nil, "a vouched email is required when authenticating with an org-scoped agent install key; send it in the Gram-User-Email header")
		}
	} else if authCtx.Email != nil {
		email = conv.NormalizeEmail(*authCtx.Email)
	}
	if email == "" {
		return oops.E(oops.CodeBadRequest, nil, "could not resolve the enrolled user's email for this scan report")
	}

	startedAt, err := time.Parse(time.RFC3339, payload.ScanStartedAt)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "scan_started_at must be a valid RFC 3339 timestamp")
	}
	completedAt, err := time.Parse(time.RFC3339, payload.ScanCompletedAt)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "scan_completed_at must be a valid RFC 3339 timestamp")
	}
	// Clamp rather than reject a reversed range: device clocks drift, and a
	// receipt from a skewed clock is still proof the device scanned.
	if completedAt.Before(startedAt) {
		completedAt = startedAt
	}

	if payload.TargetListVersion < 0 || payload.TargetListVersion > math.MaxInt32 {
		return oops.E(oops.CodeBadRequest, nil, "target_list_version must be between 0 and 2147483647")
	}
	if payload.MatchCount < 0 || payload.MatchCount > 100 {
		return oops.E(oops.CodeBadRequest, nil, "match_count must be between 0 and 100")
	}

	serial := normalizeSerial(payload.SerialNumber)
	receivedAt := time.Now().UTC()

	detections := make([]telemetry.AIDetection, 0, len(payload.Matches))
	unknownTargetIDs := make([]string, 0)
	for _, match := range payload.Matches {
		if match == nil {
			return oops.E(oops.CodeBadRequest, nil, "matches must not contain null entries")
		}
		targetID := strings.ToLower(strings.TrimSpace(match.TargetID))
		if targetID == "" {
			return oops.E(oops.CodeBadRequest, nil, "match target_id must not be blank")
		}
		category := strings.ToLower(strings.TrimSpace(match.Category))
		if category != "harness" && category != "local_model" {
			return oops.E(oops.CodeBadRequest, nil, "match category must be harness or local_model")
		}
		signal := strings.ToLower(strings.TrimSpace(match.Signal))
		if signal != "installed" && signal != "running" {
			return oops.E(oops.CodeBadRequest, nil, "match signal must be installed or running")
		}
		if target, known := aitargets.ByID(targetID); known {
			category = string(target.Category)
		} else {
			unknownTargetIDs = append(unknownTargetIDs, targetID)
		}
		detections = append(detections, telemetry.AIDetection{
			OrganizationID: authCtx.ActiveOrganizationID,
			TargetID:       targetID,
			DeviceSerial:   serial,
			UserEmail:      email,
			Signal:         signal,
			Category:       category,
			Version:        strings.TrimSpace(conv.PtrValOr(match.Version, "")),
			SeenAt:         receivedAt,
		})
	}
	if len(unknownTargetIDs) > 0 {
		s.logger.InfoContext(ctx, "ai scan reported target ids the catalog does not know; storing as reported",
			attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
			attr.SlogValueAny(unknownTargetIDs),
		)
	}

	if err := s.telemetry.UpsertAIDetections(ctx, detections); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error recording ai scan detections").LogError(ctx, s.logger)
	}

	if err := s.telemetry.InsertAIScanReceipt(ctx, telemetry.AIScanReceipt{
		OrganizationID:    authCtx.ActiveOrganizationID,
		DeviceSerial:      serial,
		UserEmail:         email,
		ScanStartedAt:     startedAt,
		ScanCompletedAt:   completedAt,
		TargetListVersion: int32(payload.TargetListVersion), // validated against the Int32 range above
		MatchCount:        uint32(payload.MatchCount),       // validated against [0, 100] above
		ReceivedAt:        receivedAt,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error recording ai scan receipt").LogError(ctx, s.logger)
	}

	return nil
}
