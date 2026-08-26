package enforcereply

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
)

type captureEnforcementPublisher struct {
	messages  []*riskv1.GitleaksEnforcement
	onPublish func(context.Context, *riskv1.GitleaksEnforcement) error
}

func (p *captureEnforcementPublisher) Publish(ctx context.Context, message *riskv1.GitleaksEnforcement) gcp.PublishResult {
	p.messages = append(p.messages, message)
	if p.onPublish != nil {
		if err := p.onPublish(ctx, message); err != nil {
			return gcp.NewErrPublishResult(err)
		}
	}
	return gcp.NewSuccessPublishResult()
}

func (p *captureEnforcementPublisher) Stop(context.Context) error {
	return nil
}

func TestDispatchStampsTenantContextAndFoldsOutcome(t *testing.T) {
	t.Parallel()

	te := setupInboxTest(t, "replica-dispatch")
	publisher := &captureEnforcementPublisher{messages: nil, onPublish: nil}
	publisher.onPublish = func(ctx context.Context, message *riskv1.GitleaksEnforcement) error {
		return te.writer.Write(ctx, message.GetReplyUrn(), testReply(message.GetRequestId(), gitleaksLane, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK))
	}
	dispatcher := &Dispatcher{inbox: te.inbox, gitleaksPub: publisher, waitTimeout: time.Second}

	outcome, err := dispatcher.Dispatch(t.Context(), DispatchRequest{
		OrganizationID: "org-dispatch",
		ProjectID:      "project-dispatch",
		Content:        "safe content",
		Lanes:          []Lane{gitleaksLane},
	})
	require.NoError(t, err)
	require.True(t, outcome.Complete)
	require.False(t, outcome.Deadline)
	require.NotNil(t, outcome.ByLane[gitleaksLane])
	require.Len(t, publisher.messages, 1)
	message := publisher.messages[0]
	require.Equal(t, "org-dispatch", message.GetOrganizationId())
	require.Equal(t, "project-dispatch", message.GetProjectId())
	require.Equal(t, "safe content", message.GetContent())
	require.NotEmpty(t, message.GetCreatedAt())
	_, err = time.Parse(time.RFC3339Nano, message.GetCreatedAt())
	require.NoError(t, err)
	_, err = uuid.Parse(message.GetRequestId())
	require.NoError(t, err)
	_, scanID, err := ParseReplyURN(message.GetReplyUrn())
	require.NoError(t, err)
	require.Equal(t, message.GetRequestId(), scanID)
}

func TestDispatchDeadlineIsNormalPartialOutcome(t *testing.T) {
	t.Parallel()

	te := setupInboxTest(t, "replica-dispatch-deadline")
	publisher := &captureEnforcementPublisher{messages: nil, onPublish: nil}
	dispatcher := &Dispatcher{inbox: te.inbox, gitleaksPub: publisher, waitTimeout: 25 * time.Millisecond}

	outcome, err := dispatcher.Dispatch(t.Context(), DispatchRequest{
		OrganizationID: "org-deadline",
		ProjectID:      "project-deadline",
		Content:        "safe content",
		Lanes:          []Lane{gitleaksLane},
	})
	require.NoError(t, err)
	require.False(t, outcome.Complete)
	require.True(t, outcome.Deadline)
	require.Empty(t, outcome.ByLane)
	require.Zero(t, te.inbox.Snapshot().Waiters)
}

func TestDispatchRejectsOversizedContent(t *testing.T) {
	t.Parallel()

	te := setupInboxTest(t, "replica-dispatch-oversized")
	publisher := &captureEnforcementPublisher{messages: nil, onPublish: nil}
	dispatcher := &Dispatcher{inbox: te.inbox, gitleaksPub: publisher, waitTimeout: time.Second}

	_, err := dispatcher.Dispatch(t.Context(), DispatchRequest{
		OrganizationID: "org-oversized",
		ProjectID:      "project-oversized",
		Content:        strings.Repeat("x", MaxContentBytes+1),
		Lanes:          []Lane{gitleaksLane},
	})
	require.ErrorContains(t, err, "maximum is 51200 bytes")
	require.Empty(t, publisher.messages)
}

func TestDispatchRejectsDuplicateLane(t *testing.T) {
	t.Parallel()

	te := setupInboxTest(t, "replica-dispatch-duplicate")
	publisher := &captureEnforcementPublisher{messages: nil, onPublish: nil}
	dispatcher := &Dispatcher{inbox: te.inbox, gitleaksPub: publisher, waitTimeout: time.Second}

	_, err := dispatcher.Dispatch(t.Context(), DispatchRequest{
		OrganizationID: "org-duplicate",
		ProjectID:      "project-duplicate",
		Content:        "safe content",
		Lanes:          []Lane{gitleaksLane, gitleaksLane},
	})
	require.ErrorContains(t, err, "duplicate enforcement lane")
	require.Empty(t, publisher.messages)
}
