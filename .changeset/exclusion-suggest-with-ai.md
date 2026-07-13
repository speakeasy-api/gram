---
"server": minor
"dashboard": minor
---

Add "Suggest with AI" to the exclusion create/edit form. The risk.suggestCustomRules endpoint takes an optional `target` (`detection` default, `exclusion`) and, for exclusions, returns structured match fields (match type, match value, rule id/source filters) that the dashboard serializes into the exclusion criteria expression — regex suggestions are validated (RE2 compile, length cap) server-side before they reach the form.
