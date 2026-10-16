---
"server": minor
---

Explore and the Platform MCP analytics tools can query sensor readings through two new datasets, `sensor_evaluations` (coverage by status) and `sensor_signals` (one row per signal answer, with confidence and the sensor's minimum classifier confidence applied). Both need signals intelligence and count each reading once, whatever retries it took.
