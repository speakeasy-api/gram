---
"server": minor
"hooks": minor
---

Risk policies now act in real time in Claude Cowork. Hooks send prompt and tool events there, and the Stop transcript fills in what hooks miss, each message stored once. Warned prompts are confirmed by sending them again; warned tool calls get Claude's own permission prompt. Block messages fill policy placeholders, masking the matched value.
