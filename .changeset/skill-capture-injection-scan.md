---
"server": minor
"dashboard": minor
---

Scan captured skill manifests for prompt injection at capture time and show current-version findings on skill details. Admins can configure the existing Prompt Injection policy from the Skills page. A completed judgement records either a finding or clean coverage; unavailable judgements are retried on a later activation and never become durable clean results. Scanning never fails the upload. Coverage is usage-based rather than catalog-based, so a version no agent ever loads is never judged.
