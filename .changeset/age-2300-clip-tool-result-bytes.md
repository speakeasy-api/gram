---
"server": patch
---

The assistant runtime now clips each MCP tool result to 150 KB before it reaches the model. Previously a single oversized tool result (e.g. a verbose external MCP response) would 413 the next provider request and crash the assistant; now the result is truncated with a clear marker so the model can ask the tool to paginate or narrow its query.
