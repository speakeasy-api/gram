---
"server": minor
"dashboard": minor
"admin": patch
---

The setup wizard has a new Set up Claude Desktop group with two cards.

- **Register Speakeasy's plugins in Claude** walks syncing the plugin marketplace in Claude.ai, marking the observability plugin Required, choosing how MCP plugins install, and allowing Speakeasy in Claude's network egress settings. Claude Desktop runs only plugins registered on the Claude organization, not a copy the device agent installs.
- **Add Claude organization instructions** gives the organization instruction that moves every conversation in Claude's unified view into Cowork, where the observability plugin captures it, and explains how to check it. It shows only for Claude Team: Enterprise captures Claude sessions through the Compliance API and inference hooks.

Both cards complete on their own once the first Cowork transcript arrives. Set up Anthropic admin controls now covers Claude Code server-managed settings, Cowork native monitoring and the Organization Gateway, and its traffic check points Cowork to Agent Sessions. Setup cards can now name the plans they need, so a card shows only when the organization's recorded stack has one of them. The support matrix gains a Cowork organization instructions method, and the Anthropic Enterprise Rollout preset includes the new group.
