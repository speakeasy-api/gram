---
"@gram-ai/functions": minor
"@gram-ai/create-function": minor
---

Added a `fromGram` utility to the Gram Functions TypeScript SDK that converts an
instance of the `Gram` mini-framework into an MCP server. This reduces the
amount of boilerplate we emit in new projects that use the `gram-template-gram`
template.
