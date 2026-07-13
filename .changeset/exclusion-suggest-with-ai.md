---
"server": minor
"dashboard": minor
---

Add "Suggest with AI" to the exclusion create/edit form, backed by a new dedicated `risk.suggestExclusion` endpoint (separate from `risk.suggestCustomRules`). It returns structured match fields (match type, match value, rule id/source filters) that the dashboard serializes into the exclusion criteria expression — regex suggestions are validated (RE2 compile, length cap) server-side before they reach the form.
