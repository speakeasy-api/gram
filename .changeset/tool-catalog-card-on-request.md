---
"dashboard": patch
---

The Project Assistant's tool catalog card now appears only when the user asks what the assistant can do. Attached MCP tools are absent from the model's declared schema, so most turns open with a `tool_search` to find the tools that turn needs — and the card drew for those too, putting a browsable tool list on top of nearly every answer. `tool_search` now declares a `browse` parameter for the model to set when the user asked what is available, and only a search carrying it renders the card; a discovery search collapses into the ordinary tool row it shares with the rest of a run's mechanics.
