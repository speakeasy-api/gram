---
"server": patch
---

Export OTel metrics as delta temporality for Datadog. The exporter previously defaulted to cumulative temporality, which forced the per-node Datadog Agent to do a stateful cumulative-to-delta conversion that corrupted counter values in our horizontally scaled deployment. Counters now emit delta at the SDK (UpDownCounters stay cumulative), making each pod self-contained and the Agent a pass-through.
