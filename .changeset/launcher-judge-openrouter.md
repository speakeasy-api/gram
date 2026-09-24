---
"server": patch
---

Add the `launcher.judge` management endpoint (`POST /rpc/launcher.judge`), which routes command palette intent decisions to Jev via OpenRouter. The dashboard sends the typed query and a short list of fuzzy-prefiltered candidates; the server frames target, action and readiness questions for TypeSafe's System One judge, pays with the organization's internal OpenRouter key, and returns probability distributions keyed by the caller's candidate ids. Organizations without a usable OpenRouter key get `disabled: true` and no outbound call is made. Person candidates are never forwarded.
