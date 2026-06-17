package assistants

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/assistants"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func projectWriteGrant(projectID uuid.UUID) authz.Grant {
	return authz.Grant{Scope: authz.ScopeProjectWrite, Selector: authz.NewSelector(authz.ScopeProjectWrite, projectID.String())}
}

func projectReadGrant(projectID uuid.UUID) authz.Grant {
	return authz.Grant{Scope: authz.ScopeProjectRead, Selector: authz.NewSelector(authz.ScopeProjectRead, projectID.String())}
}

// fakeDashboardIngestor stands in for the triggers App: it records the call and
// reproduces IngestDirect's observable effect by enqueuing the dashboard turn
// through the same EnqueueTriggerTask path the real dispatcher uses, so the
// thread the endpoint reports actually exists.
type fakeDashboardIngestor struct {
	core         *ServiceCore
	assistantID  uuid.UUID
	lastInstance uuid.UUID
	lastPayload  []byte
}

func (f *fakeDashboardIngestor) IngestDirect(ctx context.Context, instanceID uuid.UUID, payload []byte, _ time.Time) (*bgtriggers.Task, error) {
	f.lastInstance = instanceID
	f.lastPayload = payload

	var ev dashboardIngestPayload
	if err := json.Unmarshal(payload, &ev); err != nil {
		return nil, fmt.Errorf("decode dashboard payload: %w", err)
	}
	res, err := f.core.EnqueueTriggerTask(ctx, bgtriggers.Task{
		TargetKind:        bgtriggers.TargetKindAssistant,
		TargetRef:         f.assistantID.String(),
		DefinitionSlug:    sourceKindDashboard,
		EventID:           uuid.NewString(),
		CorrelationID:     ev.CorrelationID,
		EventJSON:         payload,
		TriggerInstanceID: instanceID.String(),
	})
	if err != nil {
		return nil, err
	}
	if !res.ShouldSignal {
		return nil, nil
	}
	return &bgtriggers.Task{}, nil
}

func TestSendMessageEnqueues(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID, _ := newRBACServiceWithConn(t, "assistants_send_message")
	ctx = authztest.WithExactGrants(t, ctx, projectWriteGrant(projectID))

	managed, err := svc.core.EnableManagedAssistant(ctx, "org-test", projectID, "user-test")
	require.NoError(t, err)

	ingestor := &fakeDashboardIngestor{core: svc.core, assistantID: managed.ID}
	svc.core.SetDashboardIngestor(ingestor)

	// No chat id: starts a new conversation; the server mints and returns one.
	res, err := svc.SendMessage(ctx, &gen.SendMessagePayload{
		AssistantID: managed.ID.String(),
		Message:     "what are my top errors?",
	})
	require.NoError(t, err)
	require.True(t, res.Accepted)
	require.NotEmpty(t, res.ChatID)
	require.NotNil(t, res.ThreadID)
	require.NotEmpty(t, *res.ThreadID)

	// Routed through the assistant's dashboard trigger instance, carrying the
	// user's message.
	require.NotEqual(t, uuid.Nil, ingestor.lastInstance)
	require.Contains(t, string(ingestor.lastPayload), "what are my top errors?")
}

// project:read alone is sufficient to send a message: a viewer of a project
// must be able to talk to its assistants even without project:write.
func TestSendMessageAllowedWithProjectReadOnly(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID, _ := newRBACServiceWithConn(t, "assistants_send_message_read_only")
	ctx = authztest.WithExactGrants(t, ctx, projectReadGrant(projectID))

	managed, err := svc.core.EnableManagedAssistant(ctx, "org-test", projectID, "user-test")
	require.NoError(t, err)
	svc.core.SetDashboardIngestor(&fakeDashboardIngestor{core: svc.core, assistantID: managed.ID})

	res, err := svc.SendMessage(ctx, &gen.SendMessagePayload{
		AssistantID: managed.ID.String(),
		Message:     "hello",
	})
	require.NoError(t, err)
	require.True(t, res.Accepted)
}

// Each send without a chat id starts a fresh conversation with its own
// server-minted chat + thread.
func TestSendMessageNewConversationsGetDistinctChats(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID, _ := newRBACServiceWithConn(t, "assistants_send_message_distinct")
	ctx = authztest.WithExactGrants(t, ctx, projectWriteGrant(projectID))

	managed, err := svc.core.EnableManagedAssistant(ctx, "org-test", projectID, "user-test")
	require.NoError(t, err)
	svc.core.SetDashboardIngestor(&fakeDashboardIngestor{core: svc.core, assistantID: managed.ID})

	first, err := svc.SendMessage(ctx, &gen.SendMessagePayload{
		AssistantID: managed.ID.String(),
		Message:     "hello",
	})
	require.NoError(t, err)

	second, err := svc.SendMessage(ctx, &gen.SendMessagePayload{
		AssistantID: managed.ID.String(),
		Message:     "starting over",
	})
	require.NoError(t, err)

	require.NotEqual(t, first.ChatID, second.ChatID, "each new conversation mints a distinct chat id")
	require.NotNil(t, first.ThreadID)
	require.NotNil(t, second.ThreadID)
	require.NotEqual(t, *first.ThreadID, *second.ThreadID)
}

// Passing a chat id (what listChats exposes) continues that conversation: the
// server uses the id directly, landing on the same chat + thread.
func TestSendMessageContinuesByChatID(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID, _ := newRBACServiceWithConn(t, "assistants_send_message_chatid")
	ctx = authztest.WithExactGrants(t, ctx, projectWriteGrant(projectID))

	managed, err := svc.core.EnableManagedAssistant(ctx, "org-test", projectID, "user-test")
	require.NoError(t, err)
	svc.core.SetDashboardIngestor(&fakeDashboardIngestor{core: svc.core, assistantID: managed.ID})

	first, err := svc.SendMessage(ctx, &gen.SendMessagePayload{
		AssistantID: managed.ID.String(),
		Message:     "hello",
	})
	require.NoError(t, err)

	again, err := svc.SendMessage(ctx, &gen.SendMessagePayload{
		AssistantID: managed.ID.String(),
		Message:     "follow up",
		ChatID:      new(first.ChatID),
	})
	require.NoError(t, err)
	require.Equal(t, first.ChatID, again.ChatID, "chat_id continues the same conversation")
	require.NotNil(t, first.ThreadID)
	require.NotNil(t, again.ThreadID)
	require.Equal(t, *first.ThreadID, *again.ThreadID)
}

func TestSendMessageRequiresAssistant(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID, _ := newRBACServiceWithConn(t, "assistants_send_message_404")
	svc.core.SetDashboardIngestor(&fakeDashboardIngestor{core: svc.core})
	ctx = authztest.WithExactGrants(t, ctx, projectWriteGrant(projectID))

	_, err := svc.SendMessage(ctx, &gen.SendMessagePayload{
		AssistantID: uuid.New().String(),
		Message:     "hello",
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestSendMessageRequiresProjectGrant(t *testing.T) {
	t.Parallel()

	svc, ctx, _ := newRBACService(t)
	ctx = authztest.WithExactGrants(t, ctx) // no grants

	_, err := svc.SendMessage(ctx, &gen.SendMessagePayload{
		AssistantID: uuid.New().String(),
		Message:     "hello",
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
