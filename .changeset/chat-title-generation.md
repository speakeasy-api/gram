---
"server": patch
---

Agent sessions get generated chat titles again. Sessions captured through unified hook ingest never asked for a title, so they kept the truncated first prompt they were seeded with, and archived Anthropic inference conversations all read "Claude inference conversation". Both paths now schedule title generation, which recognizes those stand-in titles and replaces them while leaving manually chosen names and channel labels alone. Titles are generated with a small fast model over a bounded slice of the transcript, so an agent session with multi-kilobyte turns no longer times out before it is named.
