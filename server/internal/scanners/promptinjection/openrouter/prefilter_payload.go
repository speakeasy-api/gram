package openrouter

import (
	"encoding/json"
	"fmt"
	"unicode/utf8"

	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	typesafe "github.com/speakeasy-api/gram/server/internal/thirdparty/typesafedecisions"
)

// Estimate tokens as serialized Unicode characters / 4, including all
// questions. This English-text heuristic can undercount code and non-English
// input; leave headroom below Jev's 32k limit and handle explicit overflow.
const maxPrefilterInputTokens = 28000

// preparePrefilterPayload bounds the complete evidence before calling Jev.
// Truncation can hide an attack in omitted text; it is marked in the evidence.
// The confirmer still receives its independently prepared, fuller evidence.
func preparePrefilterPayload(msg judgemessage.Message, trajectory judgemessage.Trajectory, questions map[string]typesafe.Question, maxInputTokens int) ([]byte, []string, bool, error) {
	payload := judgePayload{Message: judgemessage.RenderPayload(msg), Trajectory: nil}
	if trajectory.HasContent() {
		rendered := judgemessage.RenderTrajectory(trajectory)
		payload.Trajectory = &rendered
	}
	questionJSON, err := json.Marshal(questions)
	if err != nil {
		return nil, nil, false, fmt.Errorf("marshal prefilter questions: %w", err)
	}
	fields := prefilterTextFields(&payload)
	truncated := payload.Message.ToolCallsTruncated
	for _, field := range fields {
		truncated = truncated || *field.truncated
	}
	for {
		prepared, err := json.Marshal(payload)
		if err != nil {
			return nil, nil, truncated, fmt.Errorf("marshal prefilter evidence: %w", err)
		}
		if estimatePrefilterTokens(prepared, questionJSON) <= maxInputTokens {
			return prepared, judgePayloadContent(payload), truncated, nil
		}
		// Reduce the largest field first, preserving small attribution/context
		// fields until the larger bodies and argument values have been reduced.
		var largest *prefilterText
		for i := range fields {
			if largest == nil || len(*fields[i].value) > len(*largest.value) {
				largest = &fields[i]
			}
		}
		const marker = "\n...[truncated]...\n"
		if largest == nil || len(*largest.value) <= len(marker) {
			return nil, nil, truncated, fmt.Errorf("prefilter metadata exceeds input budget")
		}
		value := *largest.value
		keep := (len(value) - len(marker)) / 2
		head, tail := keep*3/5, len(value)-(keep-keep*3/5)
		for head > 0 && !utf8.RuneStart(value[head]) {
			head--
		}
		for tail < len(value) && !utf8.RuneStart(value[tail]) {
			tail++
		}
		*largest.value = value[:head] + marker + value[tail:]
		*largest.truncated = true
		truncated = true
	}
}

type prefilterText struct {
	value     *string
	truncated *bool
}

func prefilterTextFields(payload *judgePayload) []prefilterText {
	msg := &payload.Message
	fields := []prefilterText{{value: &msg.Body, truncated: &msg.BodyTruncated}, {value: &msg.Decoded, truncated: &msg.BodyTruncated}}
	addTool := func(tool *judgemessage.ToolPayload) {
		if tool != nil {
			fields = append(fields,
				prefilterText{value: &tool.Name, truncated: &tool.IdentityTruncated},
				prefilterText{value: &tool.MCPServer, truncated: &tool.IdentityTruncated},
				prefilterText{value: &tool.MCPFunction, truncated: &tool.IdentityTruncated})
		}
	}
	addTool(msg.Tool)
	for i := range msg.ToolCalls {
		call := &msg.ToolCalls[i]
		addTool(call.Tool)
		fields = append(fields, prefilterText{value: &call.Arguments, truncated: &call.ArgumentsTruncated}, prefilterText{value: &call.Decoded, truncated: &call.ArgumentsTruncated})
	}
	if t := payload.Trajectory; t != nil {
		fields = append(fields,
			prefilterText{value: &t.PriorUserRequest, truncated: &t.PriorUserRequestTruncated},
			prefilterText{value: &t.PriorUserRequestDecoded, truncated: &t.PriorUserRequestTruncated},
			prefilterText{value: &t.RecentUntrustedContent, truncated: &t.RecentUntrustedContentTruncated},
			prefilterText{value: &t.RecentUntrustedContentDecoded, truncated: &t.RecentUntrustedContentTruncated})
	}
	return fields
}

// estimatePrefilterTokens rounds up the characters/4 heuristic for the complete
// serialized state and questions; UTF-8 bytes are not character counts.
func estimatePrefilterTokens(state, questions []byte) int {
	return (utf8.RuneCount(state) + utf8.RuneCount(questions) + 3) / 4
}
