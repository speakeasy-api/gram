---
"server": patch
"dashboard": patch
"@gram-ai/elements": patch
---

Fix the chat detail "Risky only" filter and rework search-within-thread. The filter previously showed nothing on threads whose findings sat on other transcript pages, and only worked for org admins via the separate risk-results endpoint. `chat.load` (risk_only) now returns `risk_seqs` — the seqs of the flagged messages — so the panel windows the full thread and filters on the authorized load (the toggle is shown only to org admins). Search now steps through every occurrence in document order — within a message's text and inside a tool call's arguments and output — with the active occurrence highlighted distinctly, instead of stepping per message and washing every hit the same colour.
