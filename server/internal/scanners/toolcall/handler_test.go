package toolcall

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/scanners/clidestructive"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestHandlerPublishesStableFindingIDs(t *testing.T) {
	t.Parallel()

	pub := gcp.NewMockPublisher[*riskv1.Finding]()
	var published []*riskv1.Finding
	pub.On("Publish", mock.Anything, mock.Anything).
		Return(gcp.NewSuccessPublishResult()).
		Run(func(args mock.Arguments) {
			finding, ok := args.Get(1).(*riskv1.Finding)
			require.True(t, ok)
			published = append(published, finding)
		})
	meterPub := gcp.NewMockPublisher[*meteringv1.MeterReading]()
	var readings []*meteringv1.MeterReading
	meterPub.On("Publish", mock.Anything, mock.Anything).
		Return(gcp.NewSuccessPublishResult()).
		Run(func(args mock.Arguments) {
			reading, ok := args.Get(1).(*meteringv1.MeterReading)
			require.True(t, ok)
			readings = append(readings, reading)
		})
	handler := NewHandler(
		testenv.NewLogger(t),
		nil,
		nil,
		clidestructive.NewScanner(),
		pub,
		metering.NewRiskRecorder(meterPub),
	)
	request := toolCallRequest()

	require.NoError(t, handler.Handle(t.Context(), request, gcp.MessageMetadata{}))
	require.NoError(t, handler.Handle(t.Context(), request, gcp.MessageMetadata{}))
	require.Len(t, published, 2)
	require.Equal(t, clidestructive.Source, published[0].GetSource())
	require.Equal(t, published[0].GetId(), published[1].GetId())
	require.Len(t, readings, 2)
	require.Equal(t, string(metering.MeterRiskCLIDestructive), readings[0].GetMeterId())
	require.Positive(t, readings[0].GetValue())
}

func TestHandlerReturnsPublishFailure(t *testing.T) {
	t.Parallel()

	publishErr := errors.New("topic unavailable")
	pub := gcp.NewMockPublisher[*riskv1.Finding]()
	pub.On("Publish", mock.Anything, mock.Anything).Return(gcp.NewErrPublishResult(publishErr))
	handler := NewHandler(
		testenv.NewLogger(t),
		nil,
		nil,
		clidestructive.NewScanner(),
		pub,
		metering.NewRiskRecorder(gcp.NewNoopPublisher[*meteringv1.MeterReading]()),
	)

	err := handler.Handle(t.Context(), toolCallRequest(), gcp.MessageMetadata{})
	require.ErrorIs(t, err, publishErr)
}

func toolCallRequest() *riskv1.ToolCallAnalysis {
	requestID := uuid.MustParse("00000000-0000-0000-0000-000000000101").String()
	messageID := uuid.MustParse("00000000-0000-0000-0000-000000000102").String()
	chatID := uuid.MustParse("00000000-0000-0000-0000-000000000103").String()
	projectID := uuid.MustParse("00000000-0000-0000-0000-000000000104").String()
	policyID := uuid.MustParse("00000000-0000-0000-0000-000000000105").String()
	organizationID := "org-test"
	policyVersion := int64(3)
	createdAt := time.Date(2026, time.October, 18, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	executionPath := "shadow_stream"
	messageType := "tool_request"
	userID := "user-test"
	toolCallID := "call-test"
	toolName := "Bash"
	arguments := `{"command":"rm -rf /tmp/example"}`

	return riskv1.ToolCallAnalysis_builder{
		RequestId:               &requestID,
		ChatMessageId:           &messageID,
		ProjectId:               &projectID,
		OrganizationId:          &organizationID,
		RiskPolicyId:            &policyID,
		RiskPolicyVersion:       &policyVersion,
		CreatedAt:               &createdAt,
		Sources:                 []string{clidestructive.Source},
		ChatId:                  &chatID,
		OriginRiskPolicyId:      &policyID,
		OriginRiskPolicyVersion: &policyVersion,
		ExecutionPath:           &executionPath,
		ToolCallId:              &toolCallID,
		ToolName:                &toolName,
		UserId:                  &userID,
		MessageType:             &messageType,
		ToolCalls: []*riskv1.ToolCallAnalysis_ToolCall{
			riskv1.ToolCallAnalysis_ToolCall_builder{
				Id:        &toolCallID,
				Name:      &toolName,
				Arguments: &arguments,
				CreatedAt: &createdAt,
				Sender:    &userID,
			}.Build(),
		},
	}.Build()
}
