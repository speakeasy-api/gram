---
"server": minor
"dashboard": minor
---

Present the project assistant's tool activity as a short, human-readable "task" line (Claude-mobile style) — e.g. "Investigating failing tool calls" — instead of a mechanical "Calling N tools" header, with the individual tool calls collapsed behind it.

Adds a stateless `chat.summarizeToolActivity` endpoint that turns a turn's tool calls (and the user's prompt) into a concise present-/past-tense label. The chat UI shows an instant heuristic label immediately and swaps in the LLM-generated summary once it resolves, falling back to the heuristic whenever the endpoint is unavailable.
