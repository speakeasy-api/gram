---
"server": patch
---

People can now approve access to an MCP server that signs in through their identity provider when the server also offers a personal sign-in. Before, the consent page reported the service as not connected and **Give access** stayed disabled, even after connecting the personal sign-in. The page now decides from the selected credential whether tools load after approval, shows that state as soon as the credential is selected, and still lists tools before approval for a credential that is already connected.
