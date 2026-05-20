---
"server": patch
---

Chat completions no longer generate hidden reasoning tokens. Previously, OpenRouter could route requests through models that produced reasoning output Gram discarded before storage — yet still billed. The proxy and every internal completion caller (chat title generation, Slack agent loop, risk policy naming, structured object completion) now explicitly disable reasoning, eliminating that silent cost without changing observed behavior.
