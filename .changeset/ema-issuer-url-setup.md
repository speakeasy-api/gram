---
"dashboard": patch
---

Setting up Enterprise Managed Auth on an MCP Authentication Provider now asks for one thing: the identity provider's issuer URL. "Set up EMA" opens the field in place, with no choice between providers and no trip to the Okta page. An Okta org URL such as `https://acme.okta.com` works, and a pasted Okta admin console URL is read as the org URL.
