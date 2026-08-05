---
"server": minor
---

Enterprise trials now close themselves. An hourly job finds each trial organization whose trial window ended without converting to a contract, returns it to the free plan, drops it from the whitelist, and disables its platform model key so it can no longer spend. Every demotion is written to the audit log under `organization:enterprise_trial_demoted`. A trial that converts before it expires is never touched, and a trial is demoted at most once. Nothing records trials yet, so the job stays idle until trial signup ships.
