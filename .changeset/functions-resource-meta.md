---
"@speakeasy-api/functions": minor
---

Resources can now set `_meta`. Pass `meta` to `resource()` or `experimental_uiResource()` to return it on `resources/list` and `resources/read`. MCP Apps UI resources can use it to declare `ui.csp`.
