---
"server": minor
---

The staff Admin MCP has a `set_organization_function_timeout` write tool. It sets one organization's function invocation limit from 30 to 600 seconds, or restores the 60-second default with `use_default`, and returns the limit before and after the change.
