---
"@gram-ai/elements": patch
---

Moved away from a hardcoded Gram API URL. It is now possible to configure the
API URL that Gram Elements uses to communicate with Gram using Vite's `define`
setting or with `ElementsConfig["apiURL"]`.
