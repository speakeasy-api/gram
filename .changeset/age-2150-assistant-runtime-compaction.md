---
"server": minor
---

Add token-aware transcript compaction to the assistant runtime: the runner reads `gram_metadata.context_window` from completion responses, and once `usage.input_tokens` crosses 80% of the window (configurable via `ASSISTANT_CONTEXT_PERCENTAGE`) it drops reasoning + failed tool results and summarises older turns through a nested model loop. The summary preserves system + context items and the most recent few turns. The compactor's adapter omits the `Gram-Chat-ID` default header so its calls bypass chat-message capture.
