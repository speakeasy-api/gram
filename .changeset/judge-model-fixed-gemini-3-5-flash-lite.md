---
"server": patch
---

Prompt-based risk policies now all run on one benchmarked judge model, Gemini 3.5 Flash Lite, and the per-policy model picker is gone. The new model catches more of what a policy asks for and misfires less often than the previous default, at lower latency. Policies keep their temperature and fail-open settings.
