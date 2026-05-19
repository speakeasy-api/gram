---
"dashboard": minor
"server": minor
---

Restore and rework the run controls in the risk-policy progress sheet so they communicate **mode** (extend vs re-analyze) separately from **scope** (recent 1,000 / custom / all).

- **Scope** picker: radio with "Recent 1,000 messages" (default), "Custom amount" (numeric input), "All messages".
- **Primary action** "Analyze new messages" — only scans messages with no analysis at the current policy version. Safe / cheap / repeatable; pressing it twice does not re-do work already done.
- **Secondary action** "Re-analyze" dropdown — bumps the policy version and re-scans the same scope. Explicit, expensive, hidden behind a click.

Backend: `triggerRiskAnalysis` now accepts `reanalyze` (default `false`). With `reanalyze=false` the workflow is signalled without bumping `risk_policy_version`, so the drain picks up only unanalyzed messages. With `reanalyze=true` the existing bump-and-redo behavior is preserved for explicit re-runs.
