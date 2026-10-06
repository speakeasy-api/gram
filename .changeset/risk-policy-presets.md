---
"server": minor
"dashboard": minor
---

Add use-case presets for risk policies. The new-policy screen asks what to protect against, with a describe box and preset cards that land in the wizard prefilled, in place of the detector-versus-prompt choice. `risk.listPresets` returns the presets and `risk.suggestPolicy` maps a plain-language description to a policy draft. The Platform MCP gains `list_risk_presets` and `suggest_risk_policy`, `create_risk_policy` accepts a `preset`, and the detector, entity, category, action and policy-type enums now carry descriptions so an agent can choose them from their meaning.
