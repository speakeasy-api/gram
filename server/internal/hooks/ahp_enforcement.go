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
	// Join scans with the same message type and tool, so that the scanner sees
	// the full text of the boundary once instead of one fragment per part.
	var merged []ahpBoundaryScan
	positions := make(map[string]int)
	for _, scan := range scans {
		// Keep a scan with a tool name and no text: tool-scoped policies can still match it.
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

// ahpItemScans makes one scan for each text part and uses the item role as the
// message type. Tool results are scanned as tool responses. The items are not
// written to the transcript.
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
			continue // Do not scan system or developer instructions.
		}
		scans = append(scans, ahpPartScans(item.Parts, kind)...)
	}
	return scans
}

// ahpPartScans makes one scan for each text part. An assistant part in the
// tool-call category is scanned as a tool request. Text parts do not identify
// the tool reliably, so the scans have no tool name. tool.before identifies it.
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
