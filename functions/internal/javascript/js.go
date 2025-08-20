package javascript

import (
	_ "embed"
)

//go:embed gram-start.mjs
var Entrypoint []byte

var DefaultFunctions = []byte(`
export async function handleToolCall(input, context) {
  return new Response(JSON.stringify({ error: "Tool calling is not implemented" }), {
    status: 501,
	headers: { "Content-Type": "application/json" },
  });
}
`[1:])
