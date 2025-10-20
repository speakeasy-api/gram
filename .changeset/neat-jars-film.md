---
"@gram-ai/functions": patch
---

Updated the `manifest()` method of the Gram Functions TS framework to avoid
JSON-serializing the input schema for tool definitions. This was a mistake since
the server is expecting a literal object for the schema.
