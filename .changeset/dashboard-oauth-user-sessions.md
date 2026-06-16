---
"dashboard": minor
---

Rework the toolset OAuth configuration UI now that the OAuth proxy provider system is removed. The "Configure OAuth" wizard keeps its structure, but its custom path now provisions a user session issuer (creating a remote_session_issuer + remote_session_client and linking the toolset) instead of an OAuth proxy server; the external-OAuth path is unchanged. The separate migrate-from-proxy modal and the Platform/Edit OAuth-proxy modals are removed.
