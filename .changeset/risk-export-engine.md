---
"server": minor
---

feat(risk): add governed risk-clustering export engine — a Temporal workflow that extracts chat transcripts and risk findings from a read-only replica pool to JSONL in object storage, with deterministic chat-level sampling, finding-centric (windowed) or full-transcript modes, and filters for org/project/timespan/policy/rule/source/severity/role/model/user. Adds an optional `GRAM_DATABASE_READ_REPLICA_URL` (read-only) and `GRAM_RISK_EXPORT_LOCAL_DIR` for the worker; the management API trigger, RBAC scope, and audit events land separately.
