---
"server": patch
---

Pin the unconditional Temporal schedule count in the worker test to the 26 schedules that `registerSchedules` installs, so the test no longer depends on catching Temporal's schedule listing mid-registration.
