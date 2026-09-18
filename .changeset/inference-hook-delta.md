---
"server": patch
---

Anthropic inference hooks now distinguish archived transcript attempts from successfully evaluated history. Only accepted content can be skipped on later deliveries; denied or interrupted assistant/tool scans are retried, while corrected transcripts can recover. Concurrent deliveries use optimistic checkpoint updates without holding database connections during scans, and repeated transcripts longer than 512 messages no longer append duplicate archive rows. Unknown or ambiguous history is conservatively rescanned within the existing request deadline.

The accepted checkpoint is a last-known-good optimization, separate from attempted messages archived for the UI. Incomplete scanner evaluations preserve existing fail-open behavior but never advance acceptance, so a later delivery retries uncertain assistant/tool content. Concurrent successful deliveries both allow: a compare-and-swap conflict leaves the winning marker untouched, while database, context, and deadline errors still propagate.
