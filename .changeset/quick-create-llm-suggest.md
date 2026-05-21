---
"server": minor
"dashboard": minor
"sdk": minor
---

Add `risk.customRules.suggest` endpoint that calls OpenRouter to turn a one-line description ("what do you want to detect?") into a prefilled custom detection rule. The dashboard's New Custom Detection Rule sheet now opens on a single textarea, calls the new endpoint, and lands the operator in the editable review form with the suggested rule_id, title, description, regex, and severity.
