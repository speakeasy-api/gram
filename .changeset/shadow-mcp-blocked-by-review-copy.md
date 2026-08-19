---
"server": minor
"dashboard": patch
---

The Shadow MCP inventory now distinguishes a server blocked for everyone from one blocked only for some. A deny-by-default policy scoped to a subset of users (audience type "targeted") no longer reports every server as "Blocked" project-wide; those servers now carry a new `restricted` access state, rendered as an orange "Restricted — Blocked for some users" badge. A denied review is also named as its own reason: "Blocked by policy & review" when a block policy already stops the server and a review also denied it, or "Blocked by review" when an allow-by-default rule blocks it solely because of the review. Servers blocked for everyone still read "Blocked" / "Blocked by policy" as before.
