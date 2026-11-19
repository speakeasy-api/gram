---
"server": patch
---

Revoking an MCP sign-in session now stops its access token within seconds, even when the revocation cache has lost the entry. Previously a revoked session could keep working for up to five minutes.
