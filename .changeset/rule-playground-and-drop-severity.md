---
"server": minor
"dashboard": minor
---

Add a rule playground: from the Detection Rules detail sheet, the operator pastes a sample into a textarea and the dashboard calls the new `risk.rules.test` endpoint which dispatches to the same scanner code (gitleaks, Presidio, prompt-injection, regex) the worker uses. The response is a list of `TestDetectionRuleMatch`es mirroring the runtime risk_result shape.

Drop the severity-override UI from the rule detail sheet. The override edit / reset affordances will return in a follow-up PR; default severity continues to render as a row badge for context.
