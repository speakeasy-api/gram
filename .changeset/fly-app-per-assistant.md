---
"server": patch
---

Assistant Fly runtime now provisions one app per assistant (with one machine per thread) instead of one app per thread. Reduces Fly app churn and speeds cold starts; reap continues to drain old per-thread apps automatically.
