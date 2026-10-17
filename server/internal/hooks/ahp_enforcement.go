package hooks

import (
	"context"
	"fmt"
	"strings"

	ahp "github.com/agenthooksprotocol/go-sdk"
	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/hookevents"
	"github.com/speakeasy-api/gram/server/internal/message"
)

type ahpBoundaryKey struct{}
type ahpBoundaryScan struct {
	text string
	kind message.Type
	tool string
}

func (s *Service) ingestAHPBoundary(ctx context.Context, payload *gen.IngestPayload, observe bool, scans []ahpBoundaryScan) (string, error) {
	// One scan per policy type/tool keeps the full boundary together and gives
	// each scan a distinct risk operation identity rather than per-part aliases.
	merged := make([]ahpBoundaryScan, 0, len(scans))
	positions := make(map[string]int)
	for _, scan := range scans {
		key := scan.kind + "\x00" + scan.tool
		if i, ok := positions[key]; ok {
			merged[i].text += "\n" + scan.text
		} else {
			positions[key] = len(merged)
			merged = append(merged, scan)
		}
	}
	return s.ingestAHP(context.WithValue(ctx, ahpBoundaryKey{}, merged), payload, observe)
}

// Context snapshots are evidence, not additional transcript messages. Keep
// their roles intact, especially tool results, which are untrusted output.
func ahpItemScans(items []*ahp.ModelVisibleItem) []ahpBoundaryScan {
	var scans []ahpBoundaryScan
	for _, item := range items {
		if item == nil {
			continue
		}
		var kind message.Type
		switch string(item.Role) {
		case "user":
			kind = message.User
		case "assistant":
			kind = message.Assistant
		case "tool":
			kind = message.ToolResponse
		default:
			continue // system/developer instructions are not user content
		}
		scans = append(scans, ahpPartScans(item.Parts, kind)...)
	}
	return scans
}

// Inline tool-call parts are requests, not assistant prose. AHP text parts
// do not carry a trustworthy tool name/call ID; tool.before owns that identity.
func ahpPartScans(parts []*ahp.ContentItem, kind message.Type) []ahpBoundaryScan {
	var scans []ahpBoundaryScan
	for _, part := range parts {
		if part == nil || !part.TextBodyPart.Present || part.TextBodyPart.Value == nil {
			continue
		}
		body := part.TextBodyPart.Value
		partKind := kind
		if kind == message.Assistant && body.Category.Value == "tool-call" {
			partKind = message.ToolRequest
		}
		scans = append(scans, ahpBoundaryScan{text: body.Text, kind: partKind, tool: ""})
	}
	return scans
}

func ahpTextParts(parts *ahp.TextParts) string {
	if parts == nil {
		return ""
	}
	var texts []string
	for _, part := range *parts {
		if part != nil && part.TextBodyPart.Present && part.TextBodyPart.Value != nil {
			texts = append(texts, part.TextBodyPart.Value.Text)
		}
	}
	return strings.Join(texts, "\n")
}

func (s *Service) evaluateAHPBoundary(ctx context.Context, event hookevents.Event, scans []ahpBoundaryScan) (string, string) {
	for _, scan := range scans {
		if result := s.scanHookEventForEnforcement(ctx, event, scan.text, scan.kind, scan.tool); result != nil {
			action := "withheld content"
			if event.RawEventType == "context.compact.before" {
				action = "blocked compaction"
			}
			if event.RawEventType == "model.request.before" {
				action = "blocked the model request"
			}
			reason := fmt.Sprintf("Speakeasy %s: matched policy %q (%s)", action, result.PolicyName, result.Description)
			if result.Action == "quarantine" {
				s.openSessionQuarantine(ctx, event, result, reason)
			}
			return reason, renderUserBlockReason(result, reason)
		}
	}
	return "", ""
}
