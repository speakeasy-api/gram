---
"dashboard": patch
---

Fix the MCP scope picker losing focus on the server a user just deselected, jumping to whichever server happens to be first in the list instead. This made it look like individual tools could no longer be picked after unchecking a server with many tools.
