---
"server": minor
---

Serve skill authoring and distribution over Platform MCP. An OAuth-authenticated client can list, read, create, and re-version skills in an explicit project, rename them, and distribute one to an exact existing plugin or assistant. Authoring alone changes nothing at runtime, and every authoring result says so and names the targets it can be distributed to. `skills.addVersion` and `skills.update` accept an optional `expected_latest_version_id` so a write against a skill that has moved on is refused as a conflict inside the write's own transaction rather than silently overwriting another author's version. Platform MCP now prepares the acting user's RBAC grants and is enforced by the authorization engine, which previously skipped enforcement for any caller without a browser session.
