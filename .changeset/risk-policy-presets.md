---
"server": minor
---

Add use-case presets for risk policies. `risk.listPresets` returns the presets and `risk.suggestPolicy` maps a plain-language description to a policy draft. The Platform MCP gains `list_risk_presets` and `suggest_risk_policy`, `create_risk_policy` accepts a `preset`, and the detector, entity, category, action and policy-type enums now carry descriptions so an agent can choose them from their meaning.
