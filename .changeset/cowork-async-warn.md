---
"server": minor
---

Risk policies now act in real time in Claude Cowork. Hooks send prompt and tool events there, and the Stop transcript fills in what hooks miss, each message stored once. A warned prompt leads with a short approval link, or is confirmed by sending it again; warned tool calls get Claude's own permission prompt. Block messages mask the matched value.
