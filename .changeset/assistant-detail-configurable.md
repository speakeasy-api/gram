---
"server": minor
"dashboard": minor
---

The assistant detail panel is now fully configurable and observable. Overview settings (name, model, concurrency, warm TTL) are editable in place. The Sessions tab shows aggregate stats (sessions, messages, cost, tokens) over a selectable time range defaulting to the last 30 days, with per-session cost in the list. Triggers expand in place to show their recent traffic via the new `triggers.listEvents` endpoint, with each dispatched event linking to the conversation it was routed to.
