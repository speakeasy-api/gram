---
"server": patch
"dashboard": patch
---

The Skills list now loads activation, efficacy, and estimated-savings metrics without calculating unused session cost or regression signals. Regression evaluation also avoids scanning raw session telemetry when only efficacy scores are needed.
