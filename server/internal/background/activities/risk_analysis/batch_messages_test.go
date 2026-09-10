package risk_analysis

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
	"google.golang.org/protobuf/proto"

	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/stokens"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestScanSurfaceIncludesToolRequestArgs(t *testing.T) {
	t.Parallel()

	req := toolReq("Bash")
	args := req.ToolCalls[0].Function.Arguments
	require.Equal(t, args, req.scanSurface())

	withContent := toolReq("Bash")
	withContent.Content = "running cleanup"
	require.Equal(t, "running cleanup\n"+args, withContent.scanSurface())

	multi := toolReq("Bash", "Write")
	require.Equal(t, args+"\n"+args, multi.scanSurface())

	emptyArgs := toolReq("Read")
	emptyArgs.ToolCalls[0].Function.Arguments = ""
	require.Empty(t, emptyArgs.scanSurface())

	user := msg(message.User)
	require.Equal(t, "content", user.scanSurface())
}

func TestMessageContentsUsesScanSurface(t *testing.T) {
	t.Parallel()

	contents := messageContents([]batchMessage{msg(message.ToolResponse), toolReq("Bash")})
	require.Equal(t, []string{"content", `{"command":"rm -rf /tmp/data"}`}, contents)
}

func TestBatchScanRequestIDPreservesLegacyBatchCorrelation(t *testing.T) {
	t.Parallel()

	policyID := uuid.MustParse("00000000-0000-0000-0000-000000000101")
	anchorID := uuid.MustParse("00000000-0000-0000-0000-000000000202")
	otherID := uuid.MustParse("00000000-0000-0000-0000-000000000303")
	message := msg(message.User)
	message.ID = anchorID

	first := AnalyzeBatchArgs{
		ProjectID:              uuid.MustParse("00000000-0000-0000-0000-000000000404"),
		OrganizationID:         "org",
		RiskPolicyID:           policyID,
		PolicyVersion:          7,
		MessageIDs:             []uuid.UUID{anchorID},
		ContentPartIDs:         nil,
		Sources:                nil,
		MessageTypes:           nil,
		PresidioEntities:       nil,
		PresidioScoreThreshold: 0,
		CustomRuleIds:          nil,
		ApprovedEmailDomains:   nil,
		BuiltinPresetsEnabled:  false,
		DetectionScopes:        nil,
	}
	rebatched := first
	rebatched.MessageIDs = []uuid.UUID{otherID, anchorID}

	require.Equal(t, uuid.MustParse("6b619ca7-c2cd-54d9-b921-59e8765bc41a"), batchScanRequestID(first, "standard"))
	require.NotEqual(t, batchScanRequestID(first, "standard"), batchScanRequestID(rebatched, "standard"))
	require.NotEqual(t, batchScanRequestID(first, "standard"), batchScanRequestID(first, "prompt_policy"))
	require.Equal(t, batchOperationID(first, message, inlineBatchExecutionPath), batchOperationID(rebatched, message, inlineBatchExecutionPath))
	require.NotEqual(t, batchOperationID(first, message, inlineBatchExecutionPath), batchOperationID(first, message, shadowStreamExecutionPath))
}

func TestContentPartProvenanceUsesRealParentMessage(t *testing.T) {
	t.Parallel()

	parentID := uuid.MustParse("00000000-0000-0000-0000-000000000501")
	partID := uuid.MustParse("00000000-0000-0000-0000-000000000502")
	chatID := uuid.MustParse("00000000-0000-0000-0000-000000000503")
	msg := batchMessage{
		ID:                     partID,
		ChatID:                 chatID,
		ParentChatMessageID:    parentID,
		ContentPart:            true,
		Type:                   message.PromptAttachment,
		Content:                "attachment",
		RawToolCalls:           nil,
		ToolCalls:              []recordedToolCall{},
		PriorUserRequest:       "",
		RecentUntrustedContent: "",
		UserID:                 "user",
		CreatedAt:              time.Time{},
		Source:                 "codex",
	}
	args := AnalyzeBatchArgs{
		ProjectID:              uuid.MustParse("00000000-0000-0000-0000-000000000504"),
		OrganizationID:         "org",
		RiskPolicyID:           uuid.MustParse("00000000-0000-0000-0000-000000000505"),
		PolicyVersion:          2,
		MessageIDs:             nil,
		ContentPartIDs:         []uuid.UUID{partID},
		Sources:                nil,
		MessageTypes:           nil,
		PresidioEntities:       nil,
		PresidioScoreThreshold: 0,
		CustomRuleIds:          nil,
		ApprovedEmailDomains:   nil,
		BuiltinPresetsEnabled:  false,
		DetectionScopes:        nil,
	}

	provenance := batchRiskProvenance(args, msg, inlineBatchExecutionPath, "request")
	require.Equal(t, chatID, provenance.ChatID)
	require.Equal(t, parentID, provenance.ChatMessageID)
	require.Equal(t, partID, provenance.ContentPartID)
	require.Empty(t, provenance.MessageLinkReason)
	require.Empty(t, provenance.PolicyLinkReason)

	msg.ParentChatMessageID = uuid.Nil
	unlinked := batchRiskProvenance(args, msg, inlineBatchExecutionPath, "request")
	require.Equal(t, uuid.Nil, unlinked.ChatMessageID)
	require.Equal(t, "content_part_unlinked", unlinked.MessageLinkReason)
}

func TestRecordBatchResultsPublishesOnlySuccessfulPositiveUsage(t *testing.T) {
	t.Parallel()

	publisher := gcp.NewMockPublisher[*meteringv1.MeterReading]()
	var published []*meteringv1.MeterReading
	publisher.On("Publish", mock.Anything, mock.Anything).
		Return(gcp.NewSuccessPublishResult()).
		Run(func(args mock.Arguments) {
			reading, ok := args.Get(1).(*meteringv1.MeterReading)
			require.True(t, ok)
			published = append(published, reading)
		})
	analyzer := &AnalyzeBatch{
		logger:                 nil,
		tracer:                 nil,
		metrics:                nil,
		db:                     nil,
		assetStorage:           nil,
		gitleaksScanner:        nil,
		stokenCodec:            stokens.NewCodec(),
		piiScanner:             nil,
		promptInjectionScanner: nil,
		shadowMCPScanner:       nil,
		judge:                  nil,
		flags:                  nil,
		presidioPub:            nil,
		gitleaksPub:            nil,
		promptInjectionPub:     nil,
		promptPolicyPub:        nil,
		customRulesPub:         nil,
		findingsPub:            nil,
		riskRecorder:           metering.NewRiskRecorder(testenv.NewLogger(t), publisher),
		customRuleScanner:      nil,
		cliDestructiveScanner:  nil,
		destructiveToolScanner: nil,
		celEng:                 nil,
		builtinPresets:         nil,
		recommended:            RecommendedSet{},
	}
	chatID := uuid.MustParse("00000000-0000-0000-0000-000000000601")
	messages := []batchMessage{msg(message.User), toolReq("Bash"), msg(message.User)}
	for i := range messages {
		messages[i].ID = uuid.MustParse(fmt.Sprintf("00000000-0000-0000-0000-%012d", i+1))
		messages[i].ChatID = chatID
	}
	args := AnalyzeBatchArgs{
		ProjectID:              uuid.MustParse("00000000-0000-0000-0000-000000000602"),
		OrganizationID:         "org",
		RiskPolicyID:           uuid.MustParse("00000000-0000-0000-0000-000000000603"),
		PolicyVersion:          4,
		MessageIDs:             nil,
		ContentPartIDs:         nil,
		Sources:                nil,
		MessageTypes:           nil,
		PresidioEntities:       nil,
		PresidioScoreThreshold: 0,
		CustomRuleIds:          nil,
		ApprovedEmailDomains:   nil,
		BuiltinPresetsEnabled:  false,
		DetectionScopes:        nil,
	}
	results := []scanners.Result{
		{Findings: []scanners.Finding{}, STokens: 17, Completed: false},
		{Findings: []scanners.Finding{}, STokens: 5, Completed: true},
		{Findings: []scanners.Finding{}, STokens: 0, Completed: true},
	}

	require.NoError(t, analyzer.recordBatchResults(t.Context(), metering.RiskGitleaks(), args, messages, results, time.Now()))
	require.Len(t, published, 1)
	require.Equal(t, int64(5), published[0].GetValue())
	require.Equal(t, messages[1].ID.String(), published[0].GetAttributes()[metering.AttributeChatMessageID])

	presidioPublisher := gcp.NewMockPublisher[*riskv1.PresidioAnalysis]()
	var request *riskv1.PresidioAnalysis
	presidioPublisher.On("Publish", mock.Anything, mock.Anything).
		Return(gcp.NewSuccessPublishResult()).
		Run(func(call mock.Arguments) {
			var ok bool
			request, ok = call.Get(1).(*riskv1.PresidioAnalysis)
			require.True(t, ok)
		})
	analyzer.presidioPub = presidioPublisher
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestActivityEnvironment()
	publish := func(ctx context.Context) error {
		return analyzer.publishPresidioScanRequests(ctx, args, messages[1:2], DefaultPresidioScoreThreshold)
	}
	env.RegisterActivity(publish)
	_, err := env.ExecuteActivity(publish)
	require.NoError(t, err)
	require.NotNil(t, request)
	require.Equal(t, messages[1].scanSurface(), request.GetContent())
	require.Equal(t, "scan_surface", request.GetFindingSurface())
	require.Equal(t, batchScanRequestID(args, "standard").String(), request.GetRequestId())
	require.Equal(t, messages[1].ChatID.String(), request.GetChatId())
	require.Equal(t, messages[1].UserID, request.GetUserId())
	require.Equal(t, shadowStreamExecutionPath, request.GetExecutionPath())
	require.Equal(t, args.RiskPolicyID.String(), request.GetOriginRiskPolicyId())
	require.Equal(t, args.PolicyVersion, request.GetOriginRiskPolicyVersion())

	var envelope meteringv1.MeterReading
	require.NoError(t, proto.Unmarshal(request.GetMeterReading(), &envelope))
	expected, err := stokens.NewCodec().Count(t.Context(), request.GetContent())
	require.NoError(t, err)
	require.Equal(t, int64(expected), envelope.GetValue())
	require.Equal(t, request.GetChatMessageId(), envelope.GetAttributes()[metering.AttributeChatMessageID])
	require.Equal(t, inlineBatchExecutionPath, published[0].GetAttributes()[metering.AttributeScanExecutionPath])
}
