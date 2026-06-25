package hooks

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hookevents"
	codexevents "github.com/speakeasy-api/gram/server/internal/hookevents/adapters/codex"
	cursorevents "github.com/speakeasy-api/gram/server/internal/hookevents/adapters/cursor"
)

func telemetryAttrsForEvent(t *testing.T, ctx context.Context, ti *testInstance, ev any, metadata *SessionMetadata) map[attr.Key]any {
	t.Helper()
	event, ok := canonicalEvent(ev)
	require.True(t, ok)
	attrs, _ := ti.service.eventWriter.buildAttributes(ctx, ev, event, metadata, "")
	return attrs
}

func claudeTelemetryAttrs(t *testing.T, ctx context.Context, ti *testInstance, payload *gen.ClaudePayload, metadata *SessionMetadata) map[attr.Key]any {
	t.Helper()
	ev, err := ti.service.normalizeClaudeHookEvent(ctx, payload, time.Now())
	require.NoError(t, err)
	require.NotNil(t, ev)
	return telemetryAttrsForEvent(t, ctx, ti, ev, metadata)
}

func (s *Service) buildCursorTelemetryAttributes(ctx context.Context, payload *gen.CursorPayload, orgID string, projectID string) map[attr.Key]any {
	metadata := &SessionMetadata{
		SessionID:   conv.PtrValOr(payload.ConversationID, ""),
		ServiceName: "Cursor",
		UserEmail:   conv.PtrValOr(payload.UserEmail, ""),
		UserID:      "",
		ClaudeOrgID: "",
		GramOrgID:   orgID,
		ProjectID:   projectID,
	}
	ev, err := cursorevents.Normalize(authContext(ctx), payload, eventContextFromMetadata(metadata), time.Now())
	if err != nil || ev == nil {
		return map[attr.Key]any{}
	}
	event, ok := canonicalEvent(ev)
	if !ok {
		return map[attr.Key]any{}
	}
	attrs, _ := s.eventWriter.buildAttributes(ctx, ev, event, metadata, "")
	return attrs
}

func (s *Service) buildCodexTelemetryAttributes(ctx context.Context, payload *gen.CodexPayload, metadata *SessionMetadata) map[attr.Key]any {
	ev, err := codexevents.Normalize(authContext(ctx), payload, eventContextFromMetadata(metadata), time.Now())
	if err != nil || ev == nil {
		return map[attr.Key]any{}
	}
	event, ok := canonicalEvent(ev)
	if !ok {
		return map[attr.Key]any{}
	}
	attrs, _ := s.eventWriter.buildAttributes(ctx, ev, event, metadata, "")
	return attrs
}

func authContext(ctx context.Context) *contextvalues.AuthContext {
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	return authCtx
}

func eventContextFromMetadata(metadata *SessionMetadata) hookevents.EventContext {
	var projectID uuid.UUID
	if metadata != nil && metadata.ProjectID != "" {
		if parsed, err := uuid.Parse(metadata.ProjectID); err == nil {
			projectID = parsed
		}
	}
	if metadata == nil {
		return hookevents.EventContext{
			OrganizationID: "",
			ProjectID:      projectID,
			User:           hookevents.User{ID: "", Email: ""},
		}
	}
	return hookevents.EventContext{
		OrganizationID: metadata.GramOrgID,
		ProjectID:      projectID,
		User: hookevents.User{
			ID:    metadata.UserID,
			Email: metadata.UserEmail,
		},
	}
}
