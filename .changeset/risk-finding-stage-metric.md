---
"server": patch
---

Risk findings now record a `stage` telemetry dimension so findings sharing a rule ID (notably prompt injection) can be split by the detection layer that produced them: heuristic (L0) vs LLM judge (L1).
