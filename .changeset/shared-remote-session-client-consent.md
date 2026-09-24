---
"server": patch
---

Fix the MCP consent page looping on "Connected elsewhere" when one remote session client is shared by remote MCP servers with different upstream URLs. Connecting now records the upstream of the server being connected.
