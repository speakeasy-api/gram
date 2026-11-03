---
"server": patch
---

Capture the user prompt of OpenClaw turns that run on OpenClaw's Codex app-server harness. That harness fires no `before_agent_run`, so the OpenClaw observability plugin now also forwards `llm_input`, and `speakeasy-hooks` reports the prompt from it once per run when `before_agent_run` did not. Prompt policies cannot block these turns, because OpenClaw fires no blocking hook on that harness.
