---
"server": minor
"dashboard": minor
---

The research agent now persists its per-action trace — every search and page fetch a run made, in order, with the outcome, the injection judge's verdict, and a bounded preview of the untrusted text it saw. The report is a run-level synthesis that drops most of what was read; the trace is what the agent actually did, surfaced on the review page under "what the agent did." No page bodies are stored, only previews, and no new inference is run — the runner already produced this and discarded it.
