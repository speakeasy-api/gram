---
"server": patch
---

Anthropic inference hooks now distinguish archived transcript attempts from successfully evaluated history. Only accepted content can be skipped on later deliveries; denied or interrupted assistant/tool scans are retried, while corrected transcripts can recover. Concurrent deliveries use optimistic checkpoint updates without holding database connections during scans, and repeated transcripts longer than 512 messages no longer append duplicate archive rows. Unknown or ambiguous history is conservatively rescanned within the existing request deadline.
