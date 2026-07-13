---
"server": minor
---

Projects can now bring their own model provider key for the risk-policy judge and the prompt-injection classifier, each as an independent key slot. Unset slots fall back to the project default key, then the platform key.
