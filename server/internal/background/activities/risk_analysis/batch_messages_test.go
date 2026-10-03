package risk_analysis

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/message"
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
