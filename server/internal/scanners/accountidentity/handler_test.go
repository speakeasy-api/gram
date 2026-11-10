package accountidentity_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/scanners/accountidentity"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestHandlerReusesFindingIDForEnrichment(t *testing.T) {
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
	handler := accountidentity.NewHandler(testenv.NewLogger(t), pub)

	first := accountIdentityRequest("00000000-0000-0000-0000-000000000201", "", 4)
	second := accountIdentityRequest("00000000-0000-0000-0000-000000000202", "person@example.com", 4)
	require.NoError(t, handler.Handle(t.Context(), first, gcp.MessageMetadata{}))
	require.NoError(t, handler.Handle(t.Context(), second, gcp.MessageMetadata{}))

	require.Len(t, published, 2)
	require.Equal(t, published[0].GetId(), published[1].GetId())
	require.Empty(t, published[0].GetMatch())
	require.Equal(t, "person@example.com", published[1].GetMatch())
	require.NotEqual(t, published[0].GetChatMessageId(), published[1].GetChatMessageId())
}

func TestHandlerPolicyVersionChangesFindingID(t *testing.T) {
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
	handler := accountidentity.NewHandler(testenv.NewLogger(t), pub)

	require.NoError(t, handler.Handle(t.Context(), accountIdentityRequest("00000000-0000-0000-0000-000000000201", "person@example.com", 4), gcp.MessageMetadata{}))
	require.NoError(t, handler.Handle(t.Context(), accountIdentityRequest("00000000-0000-0000-0000-000000000202", "person@example.com", 5), gcp.MessageMetadata{}))

	require.Len(t, published, 2)
	require.NotEqual(t, published[0].GetId(), published[1].GetId())
}

func TestHandlerReturnsPublishFailure(t *testing.T) {
	t.Parallel()

	publishErr := errors.New("topic unavailable")
	pub := gcp.NewMockPublisher[*riskv1.Finding]()
	pub.On("Publish", mock.Anything, mock.Anything).Return(gcp.NewErrPublishResult(publishErr))
	handler := accountidentity.NewHandler(testenv.NewLogger(t), pub)

	err := handler.Handle(t.Context(), accountIdentityRequest("00000000-0000-0000-0000-000000000201", "person@example.com", 4), gcp.MessageMetadata{})
	require.ErrorIs(t, err, publishErr)
}

func accountIdentityRequest(messageID, email string, policyVersion int64) *riskv1.AccountIdentityAnalysis {
	requestID := uuid.MustParse("00000000-0000-0000-0000-000000000210").String()
	chatID := uuid.MustParse("00000000-0000-0000-0000-000000000211").String()
	projectID := uuid.MustParse("00000000-0000-0000-0000-000000000212").String()
	policyID := uuid.MustParse("00000000-0000-0000-0000-000000000213").String()
	organizationID := "org-test"
	accountType := "personal"

	return riskv1.AccountIdentityAnalysis_builder{
		RequestId:         &requestID,
		ChatMessageId:     &messageID,
		ChatId:            &chatID,
		ProjectId:         &projectID,
		OrganizationId:    &organizationID,
		RiskPolicyId:      &policyID,
		RiskPolicyVersion: &policyVersion,
		AccountType:       &accountType,
		Email:             &email,
		ApprovedDomains:   []string{"example.com"},
	}.Build()
}
