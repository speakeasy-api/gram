---
"server": patch
---

Add the `assistant_dashboard_messages` table — the user-visible conversation log for the AI Insights sidebar (user messages + the assistant's delivered replies), kept separate from the raw `chat_messages` transcript. Keyed by chat with a monotonic `seq` for incremental polling. Foundation for AGE-2631.
