---
"server": minor
---

Add an OpenRouter platform key lockdown. A locked-down key fails at key resolution with a distinct `inference_disabled` error rather than an upstream rejection, and a limit refresh reinstates it.
