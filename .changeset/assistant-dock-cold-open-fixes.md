---
"dashboard": patch
"@gram-ai/elements": patch
---

Fix the Project Assistant dock losing or duplicating the first message sent after a cold page load.

Elements: the history-enabled runtime now mounts immediately instead of waiting for auth — the previous auth gate swapped the without-history runtime for the history one when the session resolved, replacing the runtime and wiping any message sent into the first. The thread-list adapter resolves request headers through an async `getHeaders` that awaits the session fetch, so its bind-time `chat.list` waits for auth instead of failing. The custom transport is also resolved in its own memo so churn in the default transport's dependencies (MCP tool discovery settling, auth, connection status) no longer changes the transport identity mid-turn, which rebuilt the per-thread runtimes and discarded in-flight optimistic messages.

Dashboard: the dock's queued-prompt bridge appends exactly once — a throw from the placeholder thread core (before the real core binds) leaves the prompt queued for retry, while a successful append never re-fires, fixing both the dropped first message and the duplicate sends that minted a fresh chat per attempt. The server-assistant transport keeps at most one chat.load poll loop alive per dock: each send aborts the previous turn's poller, so a turn that never reaches a terminal row no longer leaves zombie polling loops behind.
