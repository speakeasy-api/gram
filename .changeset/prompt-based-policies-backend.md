---
"server": minor
---

Add prompt-based (LLM-judge) risk policies. Risk policies gain a `policy_type` discriminator (`standard` | `prompt_based`) plus `prompt` and `model_config` fields. A new `llm_judge` evaluation is wired into the realtime enforcement scanner (scoped to tool-call messages) and the batch analyzer, with findings flowing into `risk_results`. The feature is gated behind the `gram-prompt-policies` flag.
