---
"server": patch
---

Assistant chat completions no longer generate hidden reasoning tokens. Previously, OpenRouter could route assistant turns through Anthropic variants that produced reasoning output the capture pipeline discarded before storage — yet still billed. The runtime now explicitly disables reasoning on outbound completions, eliminating that silent cost without changing observed assistant behavior.
