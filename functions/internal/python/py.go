package python

var Entrypoint []byte

var DefaultFunctions = []byte(`
import json

async def handle_tool_call(input, context):
	return {
		"status": 501,
		"headers": {"Content-Type": "application/json"},
		"body": json.dumps({"error": "Tool calling is not implemented"})
	}
`[1:])
