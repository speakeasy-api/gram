---
"server": minor
---

Scan captured skill manifests for prompt injection at capture time, recording the outcome as an ordinary `risk_results` row anchored on a new nullable `skill_version_id`. A completed judgement always records a row — a hit as `found = TRUE`, a clean verdict as `found = FALSE` — so coverage is distinguishable from "never judged"; a judge that never answers records nothing rather than a durable claim that unjudged content is safe. Scanning never fails the upload. Coverage is usage-based rather than catalog-based, so a version no agent ever loads is never judged.
