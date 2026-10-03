---
"server": patch
---

Anthropic inference hook transcripts delivered without a session id now continue the conversation that already holds their history instead of starting a new one on every request. Products that omit the session id, such as Claude Design, previously produced one chat per inference call with the full transcript copied into each, which multiplied stored messages, risk scans and model calls by the length of the conversation.
