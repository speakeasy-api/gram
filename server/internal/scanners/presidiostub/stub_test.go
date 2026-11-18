package presidiostub_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/requestreply"
	"github.com/speakeasy-api/gram/server/internal/risk/enforcereply"
	"github.com/speakeasy-api/gram/server/internal/scanners/presidiostub"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type captureWriter struct {
	to      []string
	replies []*riskv1.EnforcementReply
}

func (w *captureWriter) Reply(_ context.Context, to string, reply *riskv1.EnforcementReply) error {
	w.to = append(w.to, to)
	w.replies = append(w.replies, reply)
	return nil
}

func request(createdAt time.Time) *riskv1.PresidioEnforcement {
	return riskv1.PresidioEnforcement_builder{
		RequestId:      new("req"),
		OrganizationId: new("org"),
		ProjectId:      new("project"),
		CreatedAt:      new(createdAt.UTC().Format(time.RFC3339Nano)),
	}.Build()
}

func TestEnforceHandlerRepliesOKWithNoFindings(t *testing.T) {
	t.Parallel()

	writer := &captureWriter{}
	handler := presidiostub.NewEnforceHandler(testenv.NewLogger(t), writer)
	urn := enforcereply.ReplyURN("replica", "correlation")

	err := handler.Handle(t.Context(), request(time.Now()), gcp.MessageMetadata{
		Attributes: map[string]string{requestreply.ReplyURNAttribute: urn},
	})
	require.NoError(t, err)
	require.Equal(t, []string{urn}, writer.to)
	reply := writer.replies[0]
	require.Equal(t, "correlation", reply.GetCorrelationId())
	require.Equal(t, riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_PRESIDIO, reply.GetScanner())
	require.Equal(t, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK, reply.GetStatus())
	require.Empty(t, reply.GetFindings())
	require.False(t, reply.GetIncomplete())
}

func TestEnforceHandlerDropsStaleRequests(t *testing.T) {
	t.Parallel()

	writer := &captureWriter{}
	handler := presidiostub.NewEnforceHandler(testenv.NewLogger(t), writer)

	err := handler.Handle(t.Context(), request(time.Now().Add(-time.Hour)), gcp.MessageMetadata{
		Attributes: map[string]string{requestreply.ReplyURNAttribute: enforcereply.ReplyURN("replica", "correlation")},
	})
	require.NoError(t, err)
	require.Empty(t, writer.replies)
}

func TestEnforceHandlerRejectsMissingReplyURN(t *testing.T) {
	t.Parallel()

	writer := &captureWriter{}
	handler := presidiostub.NewEnforceHandler(testenv.NewLogger(t), writer)

	err := handler.Handle(t.Context(), request(time.Now()), gcp.MessageMetadata{})
	require.Error(t, err)
	require.Empty(t, writer.replies)
}
