---
"server": minor
---

Risk: add `risk.evals.evaluate` (`evaluatePromptGuardrail`), a read-only endpoint that replays an inline prompt guardrail (prompt + judge model config + message-type scope) against a single chat session's latest generation and returns the LLM judge's per-message verdict. It powers the policy-eval workbench's tune-against-real-transcripts loop and judges an unsaved draft before a policy exists. The path writes no risk_results, publishes nothing to the outbox, and never enforces — it reuses the same judge and message flattening the realtime scanner runs.
