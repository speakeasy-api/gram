package telemetry

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	telemetryv1 "github.com/speakeasy-api/gram/infra/gen/gram/telemetry/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	otelrepo "github.com/speakeasy-api/gram/server/internal/otel/chrepo"
)

// SessionObservedHandler projects imported transcript evidence through the
// same enriched telemetry writer as hook events. Capture is not usage: these
// events deliberately carry no token, cost, completion, or tool-call counts.
type SessionObservedHandler struct {
	db     chatrepo.DBTX
	logger *Logger
}

func NewSessionObservedHandler(db chatrepo.DBTX, logger *Logger) *SessionObservedHandler {
	return &SessionObservedHandler{db: db, logger: logger}
}

func (h *SessionObservedHandler) HandleBatch(ctx context.Context, messages []*telemetryv1.SessionObserved, _ []gcp.MessageMetadata) error {
	byProject := make(map[uuid.UUID][]uuid.UUID)
	observations := make(map[uuid.UUID]*telemetryv1.SessionObserved)
	for _, msg := range messages {
		projectID, err := uuid.Parse(msg.GetProjectId())
		if err != nil {
			return fmt.Errorf("parse observed session project: %w", err)
		}
		messageID, err := uuid.Parse(msg.GetMessageId())
		if err != nil {
			return fmt.Errorf("parse observed session message: %w", err)
		}
		byProject[projectID] = append(byProject[projectID], messageID)
		observations[messageID] = msg
	}
	var logs []LogParams
	var events []otelrepo.AgentEventRow
	enabledOrgs := make(map[string]bool)
	for projectID, ids := range byProject {
		rows, err := chatrepo.New(h.db).GetImportedSessionObservations(ctx, chatrepo.GetImportedSessionObservationsParams{ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}, MessageIds: ids})
		if err != nil {
			return fmt.Errorf("load imported session observations: %w", err)
		}
		for _, row := range rows {
			enabled, checked := enabledOrgs[row.OrganizationID]
			if !checked {
				enabled, err = h.logger.logsEnabled(ctx, row.OrganizationID)
				if err != nil {
					return fmt.Errorf("check imported session telemetry: %w", err)
				}
				enabledOrgs[row.OrganizationID] = enabled
			}
			if !enabled {
				continue
			}
			observed := observations[row.ID]
			email := row.UserEmail
			if !strings.Contains(email, "@") {
				email = observed.GetUserEmail()
			}
			var snapshot userInfoSnapshot
			logs = append(logs, LogParams{
				Timestamp:          row.CreatedAt.Time,
				ToolInfo:           ToolInfo{ProjectID: projectID.String(), OrganizationID: row.OrganizationID, URN: "chat:transcript:observed", ID: "", Name: "", DeploymentID: "", FunctionID: nil},
				UserInfo:           UserInfoByIDAndEmail(row.UserID, email),
				observedTimestamp:  time.Time{},
				resourceAttributes: nil,
				userSnapshot:       snapshot,
				Attributes: map[attr.Key]any{
					attr.EventSourceKey:              string(EventSourceHook),
					attr.HookSourceKey:               row.Source.String,
					attr.GenAIConversationIDKey:      row.ChatID.String(),
					attr.ExternalUserIDKey:           row.ExternalUserID,
					attr.GenAIResponseModelKey:       row.Model.String,
					attr.GenAIProviderNameKey:        observed.GetProvider(),
					attr.HookHostnameKey:             observed.GetHookHostname(),
					attr.AccountTypeKey:              observed.GetAccountType(),
					attr.BillingModeKey:              observed.GetBillingMode(),
					attr.Key("gram.chat.message.id"): row.ID.String(),
				},
			})
			// The analytics catalog reads agent_events. Use the provider's
			// conversation ID there, matching normalized OTEL event identity.
			sessionID := conv.Default(row.ExternalChatID.String, row.ChatID.String())
			if strings.HasPrefix(sessionID, "anthropic-inference:") {
				sessionID = row.ChatID.String()
			}
			hydrated := h.logger.hydrateUserInfo(ctx, logs[len(logs)-1])
			var event otelrepo.AgentEventRow
			event.OrganizationID, event.ProjectID = row.OrganizationID, projectID.String()
			event.OccurredAtUnixNano, event.ObservedAtUnixNano = row.CreatedAt.Time.UnixNano(), time.Now().UnixNano()
			event.RecordID, event.EventID = "transcript:"+row.ID.String(), "transcript:"+row.ID.String()
			event.SessionID = sessionID
			event.RawEventName, event.Source = "session.observed", "transcript"
			event.Surface, event.Provider, event.Model = row.Source.String, observed.GetProvider(), row.Model.String
			event.UserID, event.UserEmail = hydrated.UserInfo.UserID(), hydrated.UserInfo.Email()
			event.ExternalUserID = row.ExternalUserID
			event.AccountType, event.BillingMode = observed.GetAccountType(), observed.GetBillingMode()
			event.Roles, event.Groups = hydrated.userSnapshot.Roles, hydrated.userSnapshot.Groups
			event.DepartmentName = hydrated.userSnapshot.Attributes.DepartmentName
			event.DivisionName = hydrated.userSnapshot.Attributes.DivisionName
			event.JobTitle = hydrated.userSnapshot.Attributes.JobTitle
			event.EmployeeType = hydrated.userSnapshot.Attributes.EmployeeType
			event.CostCenterName = hydrated.userSnapshot.Attributes.CostCenterName
			event.Attributes, event.ResourceAttributes, event.ScopeAttributes = "{}", "{}", "{}"
			events = append(events, event)
		}
	}
	// Ack only after ClickHouse commits, not when its async buffer accepts the
	// request. Distinct session/message identities tolerate Pub/Sub redelivery.
	written, err := h.logger.logBulk(ctx, ctx, logs, true)
	if err != nil {
		return fmt.Errorf("write imported session observations: %w", err)
	}
	if written != len(logs) {
		return fmt.Errorf("incomplete imported session telemetry: wrote %d of %d observations", written, len(logs))
	}
	if err := otelrepo.New(h.logger.chConn).InsertAgentEvents(ctx, events); err != nil {
		return fmt.Errorf("write imported agent session observations: %w", err)
	}
	return nil
}
