---
"server": minor
"dashboard": patch
---

`chat.load` now returns a `totals` object with whole-generation trace-entry counts (`total`, `user_messages`, `assistant_messages`, `tool_calls`, `tool_results`, `risk_only`). Because the detail-sheet transcript is paginated, the filter bar previously derived its counts from the loaded page — showing e.g. "Showing 150 of 150 entries" on a 19k-message chat, and a risk count that disagreed with the (generation-scoped) risk-only transcript. The dashboard now renders these counts from the server totals. Totals are scoped to the returned generation so they stay consistent with the messages on screen.
