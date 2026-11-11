package hooks

// Asynchronous warn for Claude Cowork. The first message of a Cowork session
// is typed in chat before the session moves into the Cowork VM, so no prompt
// hook sees it. The transcript the binary ships from Stop is the first point
// the server sees that prompt. This file evaluates the upload's recent user
// prompts against the enforcing policies and returns the warn matches, which
// the binary hands back to Claude from the same Stop so the user is told in
// the conversation they just had.
//
// The prompt has already been sent, so nothing is denied and no
// acknowledgement is asked for: the warning is informational. Block and
// quarantine matches are not surfaced here; they are reported by the
// background scan like any other Cowork finding.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/risk"
)

const (
	// claudeTranscriptWarnMaxPrompts bounds how many user prompts one upload
	// evaluates. A Stop normally ships one new prompt; the bound keeps a
	// replayed transcript within the binary's Stop budget.
	claudeTranscriptWarnMaxPrompts = 5

	// claudeTranscriptWarnWindow skips prompts older than this. A fresh VM
	// replays its transcript from offset zero, and the user should not be
	// warned again about prompts from earlier in the session.
	claudeTranscriptWarnWindow = 15 * time.Minute

	// claudeTranscriptWarnTimeout bounds the evaluation of one upload. The
	// binary waits for the response inside its Stop budget, so a slow scan
	// returns no warnings rather than holding the session.
	claudeTranscriptWarnTimeout = 8 * time.Second
)

// claudeTranscriptWarning is one warn-policy match on an uploaded prompt.
// Message is the policy's rendered warning and may quote the matched value:
// it is returned to the binary only, never logged or stored.
type claudeTranscriptWarning struct {
	PolicyID   string `json:"policy_id"`
	PolicyName string `json:"policy_name"`
	Message    string `json:"message"`
}

// claudeTranscriptWarnings evaluates the recent user prompts this upload
// stored for the first time and returns one warning per matching warn policy.
// A prompt a per-event hook already stored was enforced by that hook, so it is
// not warned about again. It never fails
// the upload: a scan error is logged and yields no warning.
func (s *Service) claudeTranscriptWarnings(ctx context.Context, authCtx *contextvalues.AuthContext, sessionID string, persisted claudeTranscriptPersisted) []claudeTranscriptWarning {
	if s.riskScanner == nil || persisted.stored == 0 || authCtx == nil || authCtx.ProjectID == nil {
		return nil
	}
	prompts := claudeTranscriptRecentPrompts(persisted.newPrompts, s.now().UTC())
	if len(prompts) == 0 {
		return nil
	}

	ctx, span := s.tracer.Start(ctx, "hooks.claudeTranscriptWarnings")
	defer span.End()
	// The rows are already stored, so a retry of this upload stores nothing
	// and would never warn. Finish the scan even if the binary hangs up.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), claudeTranscriptWarnTimeout)
	defer cancel()

	var warnings []claudeTranscriptWarning
	seen := make(map[string]bool)
	for _, row := range prompts {
		result, err := s.riskScanner.ScanForEnforcement(ctx, risk.RealtimeScanRequest{
			Provenance: metering.RiskProvenance{
				OrganizationID:         authCtx.ActiveOrganizationID,
				ProjectID:              *authCtx.ProjectID,
				RiskPolicyID:           uuid.Nil,
				RiskPolicyVersion:      0,
				PolicyLinkReason:       "",
				ChatID:                 uuid.Nil,
				ExternalConversationID: sessionID,
				ChatMessageID:          uuid.Nil,
				ContentPartID:          uuid.Nil,
				MessageLinkReason:      "realtime_message_not_resolved",
				OperationID:            claudeTranscriptWarnOperationID(authCtx, row.externalID),
				ExecutionPath:          "realtime_local",
				RequestID:              "",
				MessageType:            message.User,
				HookSource:             agentVariantCowork,
				UserID:                 persisted.metadata.UserID,
				ToolCallID:             "",
				ToolName:               "",
				Model:                  "",
				Provider:               "",
			},
			Text:        row.content,
			MessageType: message.User,
			ToolName:    "",
			ToolCallID:  "",
		})
		if err != nil {
			s.logger.WarnContext(ctx, "risk scan failed for claude transcript prompt",
				attr.SlogError(err),
				attr.SlogEvent("hooks_claude_transcript_warn_scan_error"),
				attr.SlogProjectID(authCtx.ProjectID.String()),
				attr.SlogGenAIConversationID(sessionID),
			)
			return warnings
		}
		if !result.IsWarnChallenge() || seen[result.PolicyID] {
			continue
		}
		seen[result.PolicyID] = true
		warnings = append(warnings, claudeTranscriptWarning{
			PolicyID:   result.PolicyID,
			PolicyName: result.PolicyName,
			Message:    renderWarnBody(result),
		})
		// Log-safe fields only: never the matched value or the message.
		s.logger.InfoContext(ctx, "warn policy matched a claude transcript prompt",
			attr.SlogEvent("hooks_claude_transcript_warned"),
			attr.SlogProjectID(authCtx.ProjectID.String()),
			attr.SlogGenAIConversationID(sessionID),
			attr.SlogRiskPolicyID(result.PolicyID),
		)
	}
	return warnings
}

// claudeTranscriptRecentPrompts returns the upload's user prompts written
// within claudeTranscriptWarnWindow of now, newest last, at most
// claudeTranscriptWarnMaxPrompts of them.
func claudeTranscriptRecentPrompts(rows []claudeTranscriptRow, now time.Time) []claudeTranscriptRow {
	var prompts []claudeTranscriptRow
	for _, row := range rows {
		if row.role != "user" || strings.TrimSpace(row.content) == "" {
			continue
		}
		if now.Sub(row.createdAt) > claudeTranscriptWarnWindow {
			continue
		}
		prompts = append(prompts, row)
	}
	if len(prompts) > claudeTranscriptWarnMaxPrompts {
		prompts = prompts[len(prompts)-claudeTranscriptWarnMaxPrompts:]
	}
	return prompts
}

// claudeTranscriptWarnOperationID keys the scan's metering on the transcript
// entry, so a re-sent upload meters the same operation.
func claudeTranscriptWarnOperationID(authCtx *contextvalues.AuthContext, externalID string) string {
	identity := fmt.Sprintf("%s:%s:%s", authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), externalID)
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("gram:risk:claude-transcript:"+identity)).String()
}
