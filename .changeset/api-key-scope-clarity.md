---
"dashboard": patch
---

Make API key scopes readable at a glance. Creating a key now opens in a side pane rather than a modal, so the form is no longer boxed in by a fixed height, and each scope is a card that leads with the integration it exists for — calling MCP servers at runtime, setup automation, plugin telemetry, device agent rollout — with its exact grants and exclusions one click away instead of crowding the page. The descriptions were also corrected against what the API enforces: a Consumer key does not reach the toolsets service, a Producer key covers everything a Consumer key can do, and the Agent key's setup instructions are now separate from its permissions. The Chat scope is no longer offered, since nothing is provisioned against it any more; keys that already carry it keep working. The project binding list is now alphabetical rather than newest-first.
