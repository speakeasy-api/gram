package dialect

import "encoding/json"

// ToolCallOutcome is the verdict an MCP client reads for a tool call: an
// isError result is an error even when the HTTP layer answered 2xx.
func ToolCallOutcome(statusCode int, resultIsError bool) string {
	if resultIsError || statusCode < 200 || statusCode >= 300 {
		return OutcomeError
	}
	return OutcomeOK
}

// MCPResultIsError reads isError off an MCP tool result document.
func MCPResultIsError(body []byte) bool {
	var result struct {
		IsError bool `json:"isError"`
	}
	return json.Unmarshal(body, &result) == nil && result.IsError
}
