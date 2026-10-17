package hooks

import (
	"context"
	"fmt"
	"strings"

	ahp "github.com/agenthooksprotocol/go-sdk"
	"github.com/speakeasy-api/gram/server/internal/hookevents"
	"github.com/speakeasy-api/gram/server/internal/message"
)

type ahpBoundaryScan struct {
	text string
	kind message.Type
	tool string
}

func mergeAHPScans(scans []ahpBoundaryScan) []ahpBoundaryScan {
	if scans == nil {
		return nil
	}
	// One scan per policy type/tool keeps the full boundary together and gives
	// each scan a distinct risk operation identity rather than per-part aliases.
	merged := make([]ahpBoundaryScan, 0, len(scans))
	positions := make(map[string]int)
	for _, scan := range scans {
		// Named tools still need tool-scoped policy checks without text.
		if scan.text == "" && scan.tool == "" {
			continue
		}
		key := scan.kind + "\x00" + scan.tool
		if i, ok := positions[key]; ok {
			merged[i].text += "\n" + scan.text
		} else {
			positions[key] = len(merged)
			merged = append(merged, scan)
		}
	}
	return merged
}

// Context snapshots are evidence, not additional transcript messages. Keep
// their roles intact, especially tool results, which are untrusted output.
func ahpItemScans(items []*ahp.ModelVisibleItem) []ahpBoundaryScan {
	scans := make([]ahpBoundaryScan, 0)
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
		if part == nil {
			continue
		}
		text, ok := ahpBodyText(part.TextBodyPart)
		if !ok {
			continue
		}
		body := part.TextBodyPart.Value
		partKind := kind
		if kind == message.Assistant && body.Category.Value == "tool-call" {
			partKind = message.ToolRequest
		}
		scans = append(scans, ahpBoundaryScan{text: text, kind: partKind, tool: ""})
	}
	return scans
}

func ahpTextParts(parts *ahp.TextParts) string {
	if parts == nil {
		return ""
	}
	var texts []string
	for _, part := range *parts {
		if part == nil {
			continue
		}
		if text, ok := ahpBodyText(part.TextBodyPart); ok {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, "\n")
}

func (s *Service) evaluateAHPBoundary(ctx context.Context, event hookevents.Event, scans []ahpBoundaryScan) (string, string) {
	for _, scan := range scans {
		if result := s.scanHookEventForEnforcement(ctx, event, scan.text, scan.kind, scan.tool); result != nil {
			action := "withheld content"
			switch event.RawEventType {
			case "context.compact.before":
				action = "blocked compaction"
			case "model.request.before":
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
