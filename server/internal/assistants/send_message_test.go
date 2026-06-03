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
	ctx = authztest.WithExactGrants(t, ctx, projectReadGrant(projectID))

	managed, err := svc.core.EnableManagedAssistant(ctx, "org-test", projectID, "user-test")
	require.NoError(t, err)

	ingestor := &fakeDashboardIngestor{core: svc.core, assistantID: managed.ID}
	svc.core.SetDashboardIngestor(ingestor)

	res, err := svc.SendMessage(ctx, &gen.SendMessagePayload{
		AssistantID:   managed.ID.String(),
		Message:       "what are my top errors?",
		CorrelationID: "user-test",
	})
	require.NoError(t, err)
	require.True(t, res.Accepted)
	require.NotEmpty(t, res.ChatID)
	require.NotEmpty(t, res.ThreadID)

	// Routed through the assistant's dashboard trigger instance, carrying the
	// user's message.
	require.NotEqual(t, uuid.Nil, ingestor.lastInstance)
	require.Contains(t, string(ingestor.lastPayload), "what are my top errors?")
}

// A different correlation_id threads the message onto a separate conversation,
// so a client can start fresh without losing the per-user default.
func TestSendMessageCorrelationStartsNewThread(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID, _ := newRBACServiceWithConn(t, "assistants_send_message_correlation")
	ctx = authztest.WithExactGrants(t, ctx, projectReadGrant(projectID))

	managed, err := svc.core.EnableManagedAssistant(ctx, "org-test", projectID, "user-test")
	require.NoError(t, err)
	svc.core.SetDashboardIngestor(&fakeDashboardIngestor{core: svc.core, assistantID: managed.ID})

	first, err := svc.SendMessage(ctx, &gen.SendMessagePayload{
		AssistantID:   managed.ID.String(),
		Message:       "hello",
		CorrelationID: "user-test",
	})
	require.NoError(t, err)

	fresh, err := svc.SendMessage(ctx, &gen.SendMessagePayload{
		AssistantID:   managed.ID.String(),
		Message:       "starting over",
		CorrelationID: "user-test:session-2",
	})
	require.NoError(t, err)

	require.NotEqual(t, first.ThreadID, fresh.ThreadID, "a new correlation id opens a new thread")
	require.NotEqual(t, first.ChatID, fresh.ChatID)
}

func TestSendMessageRequiresAssistant(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID, _ := newRBACServiceWithConn(t, "assistants_send_message_404")
	svc.core.SetDashboardIngestor(&fakeDashboardIngestor{core: svc.core})
	ctx = authztest.WithExactGrants(t, ctx, projectReadGrant(projectID))

	_, err := svc.SendMessage(ctx, &gen.SendMessagePayload{
		AssistantID:   uuid.New().String(),
		Message:       "hello",
		CorrelationID: "user-test",
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestSendMessageRequiresProjectGrant(t *testing.T) {
	t.Parallel()

	svc, ctx, _ := newRBACService(t)
	ctx = authztest.WithExactGrants(t, ctx) // no grants

	_, err := svc.SendMessage(ctx, &gen.SendMessagePayload{
		AssistantID:   uuid.New().String(),
		Message:       "hello",
		CorrelationID: "user-test",
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
