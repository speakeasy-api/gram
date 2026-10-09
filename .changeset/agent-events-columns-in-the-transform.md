---
"server": patch
---

agent_events rows are now classified and filled in the OTel transform step, so every consumer of the normalized record sees the same event type, identity, tool, outcome and usage columns, for logs and spans alike. The deprecated input_content and output_content columns are no longer filled.
