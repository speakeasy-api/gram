package hooks

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/stretchr/testify/require"
)

func TestAHPInboundAttachmentPolicy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		prompt  string
		content string
		mode    string
		deny    bool
	}{
		{"text and restricted attachment", "safe prompt", "RESTRICTED_TEST_CONTENT", "intercept", true},
		{"restricted attachment only", "", "RESTRICTED_TEST_CONTENT", "intercept", true},
		{"clean attachment", "safe prompt", "safe attachment", "intercept", false},
		{"restricted prompt", "RESTRICTED_TEST_CONTENT", "safe attachment", "intercept", true},
		{"observe", "", "RESTRICTED_TEST_CONTENT", "observe", false},
		{"no deny capability", "", "RESTRICTED_TEST_CONTENT", "no-deny", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestHooksService(t)
			ti.service.auth = &ahpTestAuthorizer{authCtx: hookAuthContext(t, ctx)}
			scanner := &ahpContentMatchingScanner{}
			ti.service.riskScanner = scanner
			h, err := ti.service.ahpHandler()
			require.NoError(t, err)
			ref := ahpUploadRef(t, ti.service.ahpUploadHandler(), []byte(tc.content))
			event := ahpTestInbound()
			parts := []any{}
			if tc.prompt != "" {
				parts = append(parts, ahpTestPart(tc.prompt))
			}
			parts = append(parts, ahpTestAttachmentPart("notes", "application/xml", ref))
			event["message"] = map[string]any{"sender": "user", "channel": "cli", "messages": []any{
				map[string]any{"id": "message", "role": "user", "parts": parts},
			}}
			if tc.mode == "observe" {
				w := ahpRequest(t, h, "hooks/observe", event, nil, ahpAuthHeaders)
				require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
				require.Empty(t, scanner.requests)
				return
			}
			effects := []string{"deny"}
			if tc.mode == "no-deny" {
				effects = []string{}
			}
			result := ahpBoundaryRequest(t, h, event, map[string]any{"effects": effects})
			if tc.deny {
				require.Len(t, result, 1)
				require.Equal(t, "deny", result[0]["type"])
			} else {
				require.Empty(t, result)
			}
			if tc.mode == "no-deny" {
				require.Empty(t, scanner.requests)
			} else {
				require.Len(t, scanner.requests, 1)
				require.Equal(t, message.User, scanner.requests[0].MessageType)
				want := tc.content
				if tc.prompt != "" {
					want = tc.prompt + "\n" + want
				}
				require.Equal(t, want, scanner.requests[0].Text)
			}
		})
	}
}

func TestAHPAttachmentOnlyDoesNotParentToEarlierPrompt(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	authCtx := hookAuthContext(t, ctx)
	ti.service.auth = &ahpTestAuthorizer{authCtx: authCtx}
	h, err := ti.service.ahpHandler()
	require.NoError(t, err)
	sessionID := uuid.NewString()
	prior := ahpTestInbound()
	prior["id"] = uuid.NewString()
	prior["session"] = map[string]any{"id": sessionID}
	w := ahpRequest(t, h, "hooks/observe", prior, nil, ahpAuthHeaders)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	queries := chatRepo.New(ti.conn)
	chatID := sessionIDToUUID(sessionID)
	msgs, err := queries.ListChatMessages(ctx, chatRepo.ListChatMessagesParams{ChatID: chatID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	require.Equal(t, "user", msgs[0].Role)
	ref := ahpUploadRef(t, ti.service.ahpUploadHandler(), []byte("attachment without a prompt"))
	event := ahpTestInbound()
	event["id"] = uuid.NewString()
	event["session"] = map[string]any{"id": sessionID}
	event["message"] = map[string]any{"sender": "user", "channel": "cli", "messages": []any{
		map[string]any{"id": "message", "role": "user", "parts": []any{ahpTestAttachmentPart("notes", "application/xml", ref)}},
	}}
	w = ahpRequest(t, h, "hooks/observe", event, nil, ahpAuthHeaders)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	parts, err := queries.ListChatContentPartsByChatID(ctx, chatRepo.ListChatContentPartsByChatIDParams{
		ChatID: chatID, ProjectID: *authCtx.ProjectID, ParentChatMessageIds: []uuid.UUID{},
	})
	require.NoError(t, err)
	require.Len(t, parts, 1)
	require.False(t, parts[0].ParentChatMessageID.Valid, "must not associate with the earlier user prompt")
}

func TestAHPInboundAttachmentWarnAcknowledgement(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.auth = &ahpTestAuthorizer{authCtx: hookAuthContext(t, ctx)}
	scanner := &ahpEvidenceScanner{stubResultScanner: stubResultScanner{result: &risk.ScanResult{
		Action: "warn", PolicyID: uuid.NewString(), PolicyName: "attachment review",
		Description: "requires review", RuleID: "attachment-review", Entity: "content",
		MatchedValue: "attachment evidence", CallFingerprint: "attachment-warn-fingerprint",
	}}}
	ti.service.riskScanner = scanner
	h, err := ti.service.ahpHandler()
	require.NoError(t, err)
	ref := ahpUploadRef(t, ti.service.ahpUploadHandler(), []byte("attachment evidence"))
	event := ahpTestInbound()
	event["message"] = map[string]any{"sender": "user", "channel": "cli", "messages": []any{
		map[string]any{"id": "message", "role": "user", "parts": []any{
			ahpTestPart("prompt evidence"), ahpTestAttachmentPart("notes", "application/xml", ref),
		}},
	}}
	caps := map[string]any{"effects": []string{"deny"}}
	effects := ahpBoundaryRequest(t, h, event, caps)
	require.Len(t, effects, 1)
	require.Equal(t, "deny", effects[0]["type"])
	require.Contains(t, effects[0]["reason"], "risk-policy-challenge/acknowledge")
	require.True(t, scanner.recordedChallenge)
	scanner.acknowledged = true
	scanner.recordedChallenge = false
	effects = ahpBoundaryRequest(t, h, event, caps)
	require.Empty(t, effects)
	require.False(t, scanner.recordedChallenge)
	require.Len(t, scanner.requests, 2)
	for _, req := range scanner.requests {
		require.Equal(t, "prompt evidence\nattachment evidence", req.Text)
		require.Equal(t, message.User, req.MessageType)
	}
}

func TestAHPInboundAttachmentEvidenceLimit(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		prompt       string
		contentBytes int
		copies       int
		deny         bool
	}{
		{"at aggregate limit", "p", ahpMaxEvidenceBytes - 2, 1, false},
		{"prompt pushes over limit", "pp", ahpMaxEvidenceBytes - 2, 1, true},
		{"repeated ref exceeds limit", "", ahpMaxEvidenceBytes / 2, 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestHooksService(t)
			ti.service.auth = &ahpTestAuthorizer{authCtx: hookAuthContext(t, ctx)}
			scanner := &ahpContentMatchingScanner{}
			ti.service.riskScanner = scanner
			h, err := ti.service.ahpHandler()
			require.NoError(t, err)
			ref := ahpUploadRef(t, ti.service.ahpUploadHandler(), []byte(strings.Repeat("a", tc.contentBytes)))
			parts := []any{}
			if tc.prompt != "" {
				parts = append(parts, ahpTestPart(tc.prompt))
			}
			for i := range tc.copies {
				parts = append(parts, ahpTestAttachmentPart(strconv.Itoa(i), "application/xml", ref))
			}
			event := ahpTestInbound()
			event["message"] = map[string]any{"sender": "user", "channel": "cli", "messages": []any{
				map[string]any{"id": "message", "role": "user", "parts": parts},
			}}
			effects := ahpBoundaryRequest(t, h, event, map[string]any{"effects": []string{"deny"}})
			if tc.deny {
				require.Len(t, effects, 1)
				require.Equal(t, "deny", effects[0]["type"])
				require.Equal(t, errAHPEvidenceSize.Error(), effects[0]["reason"])
				require.Empty(t, scanner.requests, "oversized evidence must not reach the scanner")
				w := ahpRequest(t, h, "hooks/intercept", event, []string{}, ahpAuthHeaders)
				require.Contains(t, w.Body.String(), `"error"`, "without deny capability, reject with a protocol error")
			} else {
				require.Empty(t, effects)
				require.Len(t, scanner.requests, 1)
				require.Len(t, scanner.requests[0].Text, ahpMaxEvidenceBytes)
			}
		})
	}
}
