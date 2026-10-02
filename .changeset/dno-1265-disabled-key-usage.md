---
"server": patch
---

The billing page's inference spend caps and credit meters no longer fail with a 500 when an organization's platform inference key is disabled. Usage is now read through OpenRouter's management API by key hash instead of authenticating as the key itself, which OpenRouter rejects with 401 once the key is disabled.
