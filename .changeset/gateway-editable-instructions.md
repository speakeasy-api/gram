---
"dashboard": patch
"server": minor
---

Gateway server instructions are editable: the gateway settings tab gains an Instructions section, and the metaMcp update endpoint accepts `instructions` and `instructions_mode`. Custom text is appended to Gram's built-in drill-down guidance by default, or replaces it when the mode is set to replace. Gateways without custom text keep serving the built-in text.
