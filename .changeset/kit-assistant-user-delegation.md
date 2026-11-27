---
"server": minor
---

Assistants on the Kit runtime can now act with the permissions of the person in the current turn. When `--assistant-kit-user-delegation` is on, each turn hands Kit a short-lived delegation that only the runtime's own workload identity can exchange, at the shared token endpoint, for that turn's agent credential. The agent, its owner and the person all still bound what the agent can do. A turn with no person behind it, such as a scheduled run, falls back to the workload's own access. `--assistant-kit-disabled-tools` turns off named Kit built-in tools, such as `shell` or `edit`, for every Kit session. Both flags are off by default and need a Kit release that supports session delegation.
