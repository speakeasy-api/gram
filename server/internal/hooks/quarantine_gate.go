package hooks

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hookevents"
	"github.com/speakeasy-api/gram/server/internal/risk"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/sessionquarantine"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func (s *Service) checkQuarantineGate(ctx context.Context, ev hookevents.Event) *sessionquarantine.Quarantine {
	if s.cache == nil || ev.ConversationID == "" {
		return nil
	}
	q, err := sessionquarantine.Read(
		ctx,
		s.cache,
		ev.Context.OrganizationID,
		ev.Context.ProjectID.String(),
		ev.ConversationID,
	)
	if err == nil {
		return q
	}

	s.logger.WarnContext(ctx, "session quarantine gate check failed",
		attr.SlogError(err),
		attr.SlogEvent("session_quarantine_gate_error"),
		attr.SlogHookSource(string(ev.Provider)),
		attr.SlogHookEvent(ev.RawEventType),
		attr.SlogOrganizationID(ev.Context.OrganizationID),
	)
	if s.sessionQuarantineFailClosed(ctx, ev.Context.OrganizationID) {
		return &sessionquarantine.Quarantine{
			OrganizationID: ev.Context.OrganizationID,
			ProjectID:      ev.Context.ProjectID.String(),
			SessionID:      ev.ConversationID,
			RiskPolicyID:   "",
			RiskPolicyName: "unknown",
			Reason:         "session quarantine circuit could not be checked",
			CreatedAt:      s.now().UTC(),
		}
	}
	return nil
}

func (s *Service) sessionQuarantineFailClosed(ctx context.Context, organizationID string) bool {
	if organizationID == "" || s.db == nil {
		return false
	}
	failOpen, err := riskrepo.New(s.db).IsOrganizationHooksFailOpenEnabled(ctx, organizationID)
	if err != nil {
		s.logger.WarnContext(ctx, "read session quarantine fail-closed setting",
			attr.SlogError(err),
			attr.SlogOrganizationID(organizationID),
		)
		return false
	}
	return !failOpen
}

func (s *Service) openSessionQuarantine(ctx context.Context, ev hookevents.Event, scanResult *risk.ScanResult, auditReason string) *sessionquarantine.Quarantine {
	if scanResult == nil || ev.ConversationID == "" || ev.Context.OrganizationID == "" || ev.Context.ProjectID == uuid.Nil {
		return nil
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		s.logger.WarnContext(ctx, "open session quarantine transaction", attr.SlogError(err))
		return nil
	}
	defer func() { _ = dbtx.Rollback(ctx) }()

	queries := riskrepo.New(dbtx)
	row, err := queries.CreateSessionQuarantine(ctx, riskrepo.CreateSessionQuarantineParams{
		OrganizationID: ev.Context.OrganizationID,
		ProjectID:      ev.Context.ProjectID,
		SessionID:      ev.ConversationID,
		RiskPolicyID:   conv.StringToNullUUID(scanResult.PolicyID),
		RiskPolicyName: scanResult.PolicyName,
		UserID:         ev.Context.User.ID,
		Reason:         auditReason,
	})
	created := true
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			row, err = queries.GetActiveSessionQuarantineBySession(ctx, riskrepo.GetActiveSessionQuarantineBySessionParams{
				SessionID:      ev.ConversationID,
				OrganizationID: ev.Context.OrganizationID,
				ProjectID:      ev.Context.ProjectID,
			})
			created = false
		}
		if err != nil {
			s.logger.WarnContext(ctx, "write session quarantine row",
				attr.SlogError(err),
				attr.SlogOrganizationID(ev.Context.OrganizationID),
				attr.SlogProjectID(ev.Context.ProjectID.String()),
			)
			return nil
		}
	}

	if created {
		if err := s.audit.LogSessionQuarantineOpen(ctx, dbtx, audit.LogSessionQuarantineEvent{
			OrganizationID:       row.OrganizationID,
			ProjectID:            row.ProjectID,
			Actor:                urn.NewPrincipal(urn.PrincipalTypeUser, ev.Context.User.ID),
			ActorDisplayName:     &ev.Context.User.Email,
			ActorSlug:            nil,
			SessionQuarantineURN: urn.NewSessionQuarantine(row.ID),
			RiskPolicyName:       row.RiskPolicyName,
			Metadata: audit.SessionQuarantineMetadata{
				SessionID:      row.SessionID,
				RiskPolicyID:   scanResult.PolicyID,
				RiskPolicyName: row.RiskPolicyName,
				UserID:         row.UserID,
				Reason:         row.Reason,
			},
		}); err != nil {
			s.logger.WarnContext(ctx, "audit session quarantine open", attr.SlogError(err))
			return nil
		}
	}

	if err := dbtx.Commit(ctx); err != nil {
		s.logger.WarnContext(ctx, "commit session quarantine", attr.SlogError(err))
		return nil
	}

	q := sessionquarantine.FromRow(row)
	if err := sessionquarantine.Write(ctx, s.cache, q); err != nil {
		s.logger.WarnContext(ctx, "write session quarantine circuit",
			attr.SlogError(err),
			attr.SlogOrganizationID(ev.Context.OrganizationID),
			attr.SlogProjectID(ev.Context.ProjectID.String()),
		)
	}
	return &q
}

func quarantineTriggerUserReason(scanResult *risk.ScanResult, fallback string) string {
	if scanResult == nil {
		return fallback
	}
	if scanResult.UserMessage != nil && strings.TrimSpace(*scanResult.UserMessage) != "" {
		return renderWarnBody(scanResult)
	}

	match := truncateForWarn(scanResult.MatchedValue)
	entity := scanResult.Entity
	if entity == "" {
		entity = scanResult.RuleID
	}
	if match != "" {
		return fmt.Sprintf("Your request matched policy %q: potentially harmful or sensitive content %q identified as %s. This session has been quarantined; contact your org admin to release it.",
			scanResult.PolicyName, match, entity)
	}
	return fmt.Sprintf("Your request matched policy %q: potentially harmful or sensitive content identified as %s. This session has been quarantined; contact your org admin to release it.",
		scanResult.PolicyName, entity)
}

func quarantineAuditReason(kind string, scanResult *risk.ScanResult) string {
	return fmt.Sprintf("Speakeasy quarantined this %s: matched policy %q (%s)", kind, scanResult.PolicyName, scanResult.Description)
}
