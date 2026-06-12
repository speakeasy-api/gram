---
"server": patch
"dashboard": patch
---

Attribute message type + destructured tool name to LLM-judge evaluation.

The judge now receives structured context — the message type (as an actor/role
label), and for tool calls the destructured MCP server + function — instead of
one ambiguous text field, so prompt-based policies can target message types,
actors, and specific MCP servers/functions. Also: the chat-session risk view
renders the judge rationale (instead of "llm_judge · llm_judge"), shows a
tooltip when the annotation truncates, and drops the no-op "Create exclusion"
action for judge findings.

Hardens the judge against adversarial input: the policy and message are now sent
as a single structured JSON payload framed as untrusted data, so a hostile body
can't spoof prompt headings or steer the verdict via embedded instructions;
oversized bodies are head+tail truncated before the call so a padded payload
can't blow the model's context window into a fail-open allow; and multi-tool-call
messages render each call with its own MCP attribution instead of an opaque blob.
