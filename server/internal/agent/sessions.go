package agent

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// requireSessionPortability gates the session-portability endpoints on the
// org-level product feature, so the surface stays dark until an organization
// is explicitly enrolled.
func (s *Service) requireSessionPortability(ctx context.Context, organizationID string) error {
	enabled, err := s.productFeatures.IsFeatureEnabled(ctx, organizationID, productfeatures.FeatureSessionPortability)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "check session portability feature").LogError(ctx, s.logger)
	}
	if !enabled {
		return oops.E(oops.CodeForbidden, nil, "session portability is not enabled for this organization")
	}
	return nil
}

// GetSessionMeta resolves picker metadata for captured sessions the calling
// user owns. Per-user keys only: session metadata is per-user chat data, so
// the fleet-shared org install key is refused outright — serving it there
// would let any key holder enumerate any employee's session titles via the
// vouched-email path (the DNO-383 blast-radius concern). Owner matching lives
// in the query; personal-account sessions are included for the owner (Q2
// decision) — revisit before serving content, not just titles.
func (s *Service) GetSessionMeta(ctx context.Context, payload *gen.GetSessionMetaPayload) (*gen.GetSessionMetaResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if slices.Contains(authCtx.APIKeyScopes, auth.APIKeyScopeAgent.String()) {
		return nil, oops.E(oops.CodeForbidden, nil, "session metadata requires a per-user agent key; the org install key cannot read it")
	}
	if authCtx.UserID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.requireSessionPortability(ctx, authCtx.ActiveOrganizationID); err != nil {
		return nil, err
	}

	// Map native session ids onto chat ids the same way hook ingest does, so
	// the picker and the capture pipeline can never disagree about identity.
	sessionIDByChat := make(map[uuid.UUID]string, len(payload.SessionIds))
	chatIDs := make([]uuid.UUID, 0, len(payload.SessionIds))
	for _, sessionID := range payload.SessionIds {
		sessionID = strings.TrimSpace(sessionID)
		if sessionID == "" {
			continue
		}
		chatID := chat.SessionIDToChatID(sessionID)
		if _, seen := sessionIDByChat[chatID]; seen {
			continue
		}
		sessionIDByChat[chatID] = sessionID
		chatIDs = append(chatIDs, chatID)
	}

	sessions := make([]*gen.AgentSessionMeta, 0, len(chatIDs))
	if len(chatIDs) == 0 {
		return &gen.GetSessionMetaResult{Sessions: sessions}, nil
	}

	rows, err := s.repo.ListOwnedChatSessionMeta(ctx, repo.ListOwnedChatSessionMetaParams{
		ChatIds:        chatIDs,
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         authCtx.UserID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list session metadata").LogError(ctx, s.logger)
	}

	for _, row := range rows {
		sessions = append(sessions, &gen.AgentSessionMeta{
			SessionID: sessionIDByChat[row.ID],
			ChatID:    row.ID.String(),
			Title:     conv.FromPGText[string](row.Title),
			UpdatedAt: row.UpdatedAt.Time.Format(time.RFC3339),
		})
	}

	return &gen.GetSessionMetaResult{Sessions: sessions}, nil
}

// ReportSessionMoved records a session-portability move as a
// chat_session:move audit event. It carries no session content, so — unlike
// GetSessionMeta — it accepts the org install key with a vouched email
// (mirroring GetPlugins' identity fork): fleet devices must be able to report
// moves for governance even before per-user keys roll out. The move is
// recorded whether or not the session has been captured yet.
func (s *Service) ReportSessionMoved(ctx context.Context, payload *gen.ReportSessionMovedPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}
	if err := s.requireSessionPortability(ctx, authCtx.ActiveOrganizationID); err != nil {
		return err
	}

	// Resolve the acting identity by credential type, mirroring GetPlugins.
	isInstallKey := slices.Contains(authCtx.APIKeyScopes, auth.APIKeyScopeAgent.String())
	var actor urn.Principal
	var actorDisplay *string
	if isInstallKey {
		email := conv.NormalizeEmail(conv.PtrValOr(payload.Email, ""))
		if email == "" {
			return oops.E(oops.CodeBadRequest, nil, "email is required when authenticating with an org-scoped agent install key")
		}
		actor = urn.NewPrincipal(urn.PrincipalTypeEmail, email)
		actorDisplay = conv.PtrEmpty(email)
	} else {
		if authCtx.UserID == "" {
			return oops.C(oops.CodeUnauthorized)
		}
		actor = urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)
		actorDisplay = authCtx.Email
	}

	sessionID := strings.TrimSpace(payload.SessionID)
	if sessionID == "" {
		return oops.E(oops.CodeBadRequest, nil, "session_id is required")
	}
	chatID := chat.SessionIDToChatID(sessionID)

	// Best-effort display enrichment; a not-yet-captured session still gets
	// its move recorded, just without a title or owner.
	var chatTitle, ownerUserID string
	row, err := s.repo.GetChatTitleForMove(ctx, repo.GetChatTitleForMoveParams{
		ID:             chatID,
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	switch {
	case err == nil:
		chatTitle = conv.FromPGTextOrEmpty[string](row.Title)
		ownerUserID = conv.FromPGTextOrEmpty[string](row.UserID)
	case errors.Is(err, pgx.ErrNoRows):
		s.logger.DebugContext(ctx, "session move reported before capture; recording without chat enrichment",
			attr.SlogEvent("agent_session_move_uncaptured"),
			attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
		)
	default:
		return oops.E(oops.CodeUnexpected, err, "resolve moved session").LogError(ctx, s.logger)
	}

	event := audit.LogChatSessionMoveEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            actor,
		ActorDisplayName: actorDisplay,
		ActorSlug:        nil,
		ChatSessionURN:   urn.NewChatSession(chatID),
		ChatTitle:        chatTitle,
		OwnerUserID:      ownerUserID,
		TargetHarness:    strings.TrimSpace(payload.TargetHarness),
		SourceSurface:    strings.TrimSpace(conv.PtrValOr(payload.SourceSurface, "")),
		DeviceSerial:     normalizeSerial(payload.SerialNumber),
		DeviceHostname:   strings.TrimSpace(conv.PtrValOr(payload.Hostname, "")),
	}
	if err := s.audit.LogChatSessionMove(ctx, s.db, event); err != nil {
		return oops.E(oops.CodeUnexpected, err, "record session move").LogError(ctx, s.logger)
	}

	return nil
}
