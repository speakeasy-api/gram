---
---

Remove the deprecated `gram-risk-v1-finding-bq-writer` Pub/Sub subscription declaration. The consumer was decommissioned in #4368 but the subscription was left in place, so messages published to `gram-risk-v1-finding` accumulated unacked and tripped the backlog-age monitor. Also removes the dead `DISABLE_BIGQUERY_WRITES` env var and a stale BigQuery comment left behind by the decommission. Infra-only; no package release.
