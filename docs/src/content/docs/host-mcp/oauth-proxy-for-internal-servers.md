---
title: OAuth Proxy for Internal Servers
description: Explores the option of using an OAuth Proxy for an internal facing MCP Server
sidebar:
  order: 4
---

# OAuth Proxy

For situations where an OAuth system do not yet support the MCP requirement of DCR, Gram offers an OAuth proxy that translates between MCP requirements and standard OAuth implementations. **This is essentially a workaround to avoid implementing the full MCP requirements of DCR**, but it is only viable for certain usecases.

How the OAuth proxy works:
- **Frontend**: Exposes OAuth 2.1 and DCR to MCP clients on your behalf
- **Backend**: Communicates with your OAuth APIs using a single set of credentials
- **Translation**: Handles the complexity of MCP-compliant OAuth flows

Functionally, this is very similar to solutions others might be familiar with, such as the [Cloudflare OAuth proxy](https://blog.cloudflare.com/remote-model-context-protocol-servers-mcp/#workers-oauth-provider-an-oauth-2-1-provider-library-for-cloudflare-workers).

## Important Caveats and Limitations

**Single Client Credentials**: Because your OAuth system likely requires a specific `client_id`/`client_secret`, your MCP server stores a single set of these values to act as from the proxy. This means:
- All users share the same underlying OAuth client
- You lose per-user client isolation
- Rate limiting and quotas apply to the shared client

**Suitability Considerations**: 
- ✅ **Good for**: Internal tools, single OAuth app servers, POCs and demos
- ❌ **May not be suitable for**: Public servers serving all your customers, your customers likely all have their own client_id/client_secrets. You will need some form of DCR.
- ⚠️ **Consider carefully**: Whether having all users share a single OAuth client is acceptable for your use case

**Our recommendation**: For an MCP server that publicly serves all users, implementing DCR or having customers pass in access tokens directly is typically the best option.

### OAuth Proxy for POCs and Testing

For building POCs and testing OAuth flows, we're happy to help set up an OAuth proxy with a specific `client_id`/`client_secret` pairing so you can demonstrate the OAuth flow functionality without implementing full DCR support.

**Configuration Requirements**

You will store the following in Gram to enable our OAuth proxy to interact with your underlying OAuth provider:
- Authorization Endpoint
- Token Endpoint
- Client ID & Client Secret
- List of Scopes (optional)
- Token endpoint auth methods supported (optional)
- You may need to whitelist oauth proxy redirect uris in your OAuth provider (https://{mcp_server_domain}.com/oauth/{mcp_slug}/callback)