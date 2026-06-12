---
"@gram-ai/elements": patch
---

Stop tool call cards from flashing while the assistant works. Cards no longer
reset (collapsing and re-highlighting their code) when a streaming turn grows
a single tool call into a group, and they no longer re-render on every text
chunk.
