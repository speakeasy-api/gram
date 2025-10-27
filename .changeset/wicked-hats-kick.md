---
"@gram-ai/functions": minor
---

Removed the per-tool config for declaring environment variables and instead opts
for updating the Gram class to optionally accept an input environment and an
associated zod schema for it. When a schema is defined, the code benefits from
strict types and transforms when accessing environment variables via the tool
context.
