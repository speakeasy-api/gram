---
"server": patch
"dashboard": patch
"@gram-ai/elements": patch
---

Address review feedback from the OpenRouter model refresh: pin explicit per-provider fallback models in ResolveModel so de-listed or unknown models never silently resolve to a premium model (previously anthropic/\* fell back alphabetically to Claude Fable 5), give elements an explicit DEFAULT_MODEL (Claude Sonnet 5) instead of MODELS[0], and remove Gemini 3.5 Flash from the prompt-policy judge picker (the judge disables reasoning, which that model rejects).
