---
"server": patch
---

Emit `risk.prompt_injection.insufficient_credits` and a dedicated `pi_judge_insufficient_credits` log event when the OpenRouter internal key returns HTTP 402. Fail-open still applies, but operators can now alert when PI enforcement is silently disabled instead of it surfacing as an apparent hooks-latency improvement.
