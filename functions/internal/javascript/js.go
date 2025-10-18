package javascript

import (
	_ "embed"
)

//go:embed gram-start.mjs
var Entrypoint []byte

// DefaultFunctions contains a default implementation of the functions.js file.
// It is used when custom code is not found on the runner machine. The benefit
// of having this default implementation is that it allows Gram to always get
// a useful response even in failure scenarios.
var DefaultFunctions = []byte(`
export async function handleToolCall() {
  return new Response(JSON.stringify({ error: "Tool calling is not implemented" }), {
    status: 501,
	headers: { "Content-Type": "application/json" },
  });
}

export async function handleResources() {
  return new Response(JSON.stringify({ error: "Resource handling is not implemented" }), {
    status: 501,
	headers: { "Content-Type": "application/json" },
  });
}
`[1:])
