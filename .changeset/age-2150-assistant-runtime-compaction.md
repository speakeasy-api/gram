---
"server": minor
---

Add token-aware transcript compaction to the assistant runtime: gram resolves the model's context window at admit/configure time and pushes it to the runner via `RunnerConfig.context_window`. Once `usage.input_tokens` crosses 80% of the window (configurable via `ASSISTANT_CONTEXT_PERCENTAGE`) the runner drops reasoning + failed tool results and summarises older turns through a nested model loop, preserving system + context items and the most recent few turns. The compactor's adapter omits the `Gram-Chat-ID` default header so its calls bypass chat-message capture.
