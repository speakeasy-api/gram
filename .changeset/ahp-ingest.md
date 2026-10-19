---
"server": minor
---

Add an Agent Hooks Protocol endpoint at `POST /ahp`. Harnesses that speak AHP can send tool calls, user prompts, and session lifecycle events with a hooks API key. Speakeasy policies can deny tool calls and prompts the harness intercepts.
