---
"server": minor
"dashboard": minor
---

The tokens under management endpoint now returns usage history: the trailing 12 billing cycles, each with a per-UTC-day breakdown. Chat qualification is evaluated per cycle, so daily points sum exactly to each cycle's TUM. The enterprise billing page renders this as a bar chart with day and billing-cycle granularity toggles, including a contracted-limit line in the cycle view.
