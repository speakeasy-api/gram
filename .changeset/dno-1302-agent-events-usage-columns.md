---
"server": patch
---

Token and cost usage on agent events now comes from the transform step, filled on API requests only, so every consumer of the normalized event stream sees the same usage the agent_events table stores. The deprecated input_content and output_content columns are no longer filled.
