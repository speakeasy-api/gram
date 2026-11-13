---
type: llm
focus: trace
weight: 1
---

The chart sends every repeat of a request whose outcome is not definitely known under one idempotency key, a new key is used only after a definite refusal if at all, and a Go test asserts this
