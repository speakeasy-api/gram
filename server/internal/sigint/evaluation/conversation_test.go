package evaluation

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	conversationv1 "github.com/speakeasy-api/gram/infra/gen/gram/conversation/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/sigint/repo"
	"github.com/speakeasy-api/gram/server/internal/streams"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

type countingMessages struct {
	rows     storedMessages
	requests map[string][]uuid.UUID
	err      error
}

func (s *countingMessages) LoadMessages(ctx context.Context, org string, project uuid.UUID, ids []uuid.UUID) ([]repo.LoadEvaluationMessagesRow, error) {
	s.requests[org+"/"+project.String()] = append(s.requests[org+"/"+project.String()], ids...)
	if s.err != nil {
		return nil, s.err
	}
	return s.rows.LoadMessages(ctx, org, project, ids)
}

func TestConversationBatchLoadsCreatedMessagesByTenant(t *testing.T) {
	t.Parallel()
	first, other := message(), message()
	other.SetOrganizationId("other-organization")
	second, ok := proto.Clone(first).(*conversationv1.MessageEvent)
	require.True(t, ok)
	second.SetId(uuid.NewString())
	second.SetMessageId(uuid.NewString())
	ignored, ok := proto.Clone(first).(*conversationv1.MessageEvent)
	require.True(t, ok)
	ignored.SetType(conversationv1.MessageEvent_TYPE_ATTACHMENTS_ADDED)
	ignored.SetMessageId("must-not-be-read")
	missing, ok := proto.Clone(first).(*conversationv1.MessageEvent)
	require.True(t, ok)
	missing.SetId(uuid.NewString())
	missing.SetMessageId(uuid.NewString())
	var deps dependencies
	deps.Test(t)
	t.Cleanup(func() { deps.AssertExpectations(t) })
	deps.On("IsFeatureEnabled", mock.Anything, mock.Anything, mock.Anything).Return(false, nil).Times(4)

	evaluator, err := NewEvaluator(testenv.NewLogger(t), testenv.NewMeterProvider(t), &deps, &deps, nil, nil)
	require.NoError(t, err)

	source := &countingMessages{rows: storedMessages{}, requests: map[string][]uuid.UUID{}}
	for _, m := range []*conversationv1.MessageEvent{first, second, other} {
		row := storedMessage(m)
		source.rows[row.ChatMessage.ID] = row
	}
	h := NewConversationHandler(evaluator, nil, source)
	var batch []streams.BatchMessage[*conversationv1.MessageEvent]
	for _, m := range []*conversationv1.MessageEvent{first, second, first, ignored, missing, other} {
		batch = append(batch, streams.BatchMessage[*conversationv1.MessageEvent]{Message: m})
	}
	require.NoError(t, h.HandleBatchWithResult(t.Context(), batch))
	require.Len(t, source.requests, 2)
	require.ElementsMatch(t, []uuid.UUID{uuid.MustParse(first.GetMessageId()), uuid.MustParse(second.GetMessageId()), uuid.MustParse(missing.GetMessageId())}, source.requests[first.GetOrganizationId()+"/"+first.GetProjectId()])
	require.Equal(t, []uuid.UUID{uuid.MustParse(other.GetMessageId())}, source.requests[other.GetOrganizationId()+"/"+other.GetProjectId()])
}

func TestConversationLookupFailureRetriesAndUpdatesAreIgnored(t *testing.T) {
	t.Parallel()
	source := &countingMessages{requests: map[string][]uuid.UUID{}, err: fmt.Errorf("database unavailable")}
	h := NewConversationHandler(nil, nil, source)
	m := message()
	require.ErrorContains(t, h.Handle(t.Context(), m, gcp.MessageMetadata{}), "database unavailable")
	for _, kind := range []conversationv1.MessageEvent_Type{conversationv1.MessageEvent_TYPE_UNSPECIFIED, conversationv1.MessageEvent_TYPE_ATTRIBUTION_UPDATED, conversationv1.MessageEvent_TYPE_ATTACHMENTS_ADDED} {
		m.SetType(kind)
		require.NoError(t, h.Handle(t.Context(), m, gcp.MessageMetadata{}))
	}
	require.Len(t, source.requests[m.GetOrganizationId()+"/"+m.GetProjectId()], 1)
}
