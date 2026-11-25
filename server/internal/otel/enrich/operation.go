package enrich

import (
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
)

// maxTextBytes bounds the canonical copy of a record's words, which sits
// beside the producer's own attribute: 64 KiB holds any prompt a person types
// and stays inside the headroom a relay export reserves for enrichments.
const maxTextBytes = 64 * constants.KiB

// capText cuts the canonical copy of a record's words at capBytes on a
// character boundary; the producer's own attribute is untouched.
func capText(text string, capBytes int) string {
	if len(text) <= capBytes {
		return text
	}
	return truncateUTF8(text, capBytes)
}

// impliedOutcome is the outcome an api event type carries by being that type.
func impliedOutcome(eventType string) string {
	switch eventType {
	case dialect.EventTypeAPIResponse:
		return dialect.OutcomeOK
	case dialect.EventTypeAPIError:
		return dialect.OutcomeError
	case dialect.EventTypeAPIRefusal:
		return dialect.OutcomeRefused
	default:
		return ""
	}
}

func isToolEvent(eventType string) bool {
	return eventType == dialect.EventTypeToolCall || eventType == dialect.EventTypeToolCallResult || eventType == dialect.EventTypeToolDecision
}

func carriesModel(eventType string) bool {
	switch eventType {
	case dialect.EventTypeAPIRequest, dialect.EventTypeAPIResponse, dialect.EventTypeAPIError, dialect.EventTypeAPIRefusal,
		dialect.EventTypeAPIRequestBody, dialect.EventTypeAPIResponseBody:
		return true
	default:
		return false
	}
}

// A request and a tool call keep everything in attributes, and a payload
// capture keeps its body whole, so none of them has words.
func carriesText(eventType string) bool {
	switch eventType {
	case dialect.EventTypePrompt, dialect.EventTypeAPIResponse, dialect.EventTypeAPIError, dialect.EventTypeToolDecision:
		return true
	default:
		return false
	}
}

// A request and a tool call record that a call was made, not how it went.
func statesOutcome(eventType string) bool {
	return eventType == dialect.EventTypeToolCallResult || eventType == dialect.EventTypeToolDecision || eventType == dialect.EventTypeCompaction
}

func carriesOutcomeMessage(eventType string) bool {
	return eventType == dialect.EventTypeAPIError || eventType == dialect.EventTypeToolCallResult || eventType == dialect.EventTypeCompaction
}

// A tool_call span is the whole call and has a duration; a tool_call log
// record is the call's start and has none. A compaction's stays in its payload.
func carriesDuration(eventType string) bool {
	return eventType == dialect.EventTypeAPIRequest || eventType == dialect.EventTypeToolCall || eventType == dialect.EventTypeToolCallResult
}
