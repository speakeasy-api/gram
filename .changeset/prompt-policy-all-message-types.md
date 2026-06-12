---
"server": patch
"dashboard": patch
---

Open prompt-based ("LLM-judge") risk policies to all message types.

Previously the judge was hard-scoped to `tool_request` in both the realtime
scanner and the batch analyzer, regardless of the policy's `message_types`. The
judge now runs on whatever types a policy declares (`user_message`,
`tool_request`, `tool_response`, `assistant_message`), and the policy form lets
you choose them instead of locking to tool requests.
