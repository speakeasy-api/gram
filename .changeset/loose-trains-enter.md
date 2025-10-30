---
"function-runners": patch
---

Updated the runner to detect if the default export from customer TS/JS code is a
`Promise` to an object containing `handleToolCall` / `handleResources` and
awaits it before proceeding with a tool/resource request.
