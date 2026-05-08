---
"server": patch
---

Fix generated observability plugin hooks not firing correctly in production. Hook events now carry explicit `async` flags matching the public Gram plugin (`false` for blocking events like `PreToolUse` and `UserPromptSubmit`, `true` for fire-and-forget events like `Stop` and `PostToolUse`). The generated `hook.sh` script now captures the HTTP response body and status code separately, forwarding the body to stdout for Claude to read `permissionDecision` from on `PreToolUse`, and exiting with code 2 on 4xx/5xx so an unreachable Gram server cannot silently bypass blocking policies.
