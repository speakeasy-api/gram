---
"dashboard": patch
---

Agent setup fixes:

- People whose role blocks any server can create agents again. The grant to sign in to every auth client is left out when they may not give it.
- Saving an edited agent returns to that agent's page.
- The verify step shows its green checks and hides troubleshooting once the agent connects.
- The agent's recent changes read in words rather than audit codes.
- The MCP connections table fits its panel.
- The wizard summary says what the agent can do in words, with the scopes on hover.
- A taken agent name is caught on the name step, before Create agent.
- The server info popover shows how each server signs in, listing every option it offers.
- The setup text names OpenClaw.
