---
title: Add OAuth to an MCP Server
description: Configure authentication for MCP servers using different OAuth methods
sidebar:
  order: 3
---

Starting March 2025, the MCP specification recommends OAuth-based authentication for MCP Servers. However, it's important to understand that **user-facing OAuth exchange is not actually a true requirement for an MCP server**. There are several valid approaches to authentication:

1. **Direct Access Tokens**: Passing in access tokens directly as headers to servers is completely valid
2. **Client Credentials Flow**: For server-to-server authentication 
3. **User-facing OAuth Flow**: For scenarios requiring dynamic user authentication

The Gram product exposes a variety of different options for OAuth that you can integrate into your Gram MCP Servers.
Your OpenAPI spec must include OAuth as a security option for your endpoints.

```
security:
  - oauth2Example: [pets:read]

components:
  securitySchemes:
    oauth2Example:
      type: oauth2
      flows:
        authorizationCode:
          authorizationUrl: /oauth/authorize
          tokenUrl: /oauth/token
          scopes:
            pets:read: Read pet information
            pets:write: Modify pet information
```

## Choosing Your Authentication Approach

Before implementing OAuth, consider what kind of credentials you expect end users to provide when initializing your MCP servers:

### Option 1: Pre-obtained Access Tokens (Recommended for most cases)
- **What users provide**: A valid access token they obtained from your service
- **How they get it**: Through your existing dashboard, API, or token generation system  
- **Best for**: Existing APIs with token generation, internal tools, controlled environments
- **Complexity**: Low - no OAuth flow implementation needed

### Option 2: Client Credentials (Server-to-Server)
- **What users provide**: `client_id` and `client_secret` 
- **How they get it**: Register an application in your developer portal
- **Best for**: APIs designed for server-to-server authentication
- **Complexity**: Medium - automatic token exchange and caching

### Option 3: User-facing OAuth Flow 
- **What users provide**: Nothing initially - they authenticate interactively
- **How it works**: Dynamic OAuth flow when the MCP server is accessed
- **Best for**: Public-facing servers requiring user consent
- **Complexity**: High - requires DCR implementation or OAuth proxy

**Gram can integrate OAuth into a server in any way that's currently possible within the MCP context.** The key is choosing the approach that best fits your existing authentication system and user experience goals.

## Access Token Based Authentication

**This is often the simplest and most practical approach for MCP servers.** Gram allows passing pre-obtained OAuth access tokens directly to an MCP server through headers. This is completely valid and doesn't require implementing complex OAuth flows.

This approach works with access tokens from any OAuth flow:
- `authorizationCode` - User grants permission, you exchange code for token
- `clientCredentials` - Server-to-server authentication with client credentials  
- `implicit` - Direct token generation (less secure, not recommended)

**When to use this approach:**
- You already have a system for obtaining access tokens
- You want to avoid the complexity of OAuth flows in your MCP server
- Your users can generate tokens through your existing dashboard or API
- You're building internal tools where token management is handled elsewhere

## Client Credentials Flow

If your API uses the `clientCredentials` flow, Gram allows you to pass a client_id and client_secret into your MCP server. The server will do the token exchange using those provided environment values. The tool call flow will automatically cache tokens received from a token exchange based on the expiration of that token. We natively support both `client_secret_post` and `client_secret_basic` flows.

## Authorization Code

:::tip[NOTE]
Placing Managed OAuth in front of a server is a Pro and Enterprise feature.

An MCP Server must be marked `public` to attach Managed OAuth in front of it.

Please [book in time with our team](https://calendly.com/sagar-speakeasy/30min) for white-glove service with proxy setup or DCR compliance. We'll get you up and running.
:::


When the MCP spec refers to placing a user-facing OAuth flow in front of a server, it is typically referring to the `authorizationCode` flow.

Gram fully supports registering an OAuth server in front of MCP servers for your users to interact with. Something that is important to keep in mind is that the MCP specification has very specific requirements for how a company's OAuth API needs to work. 

## Why DCR is Required for MCP

**The main requirement is that MCP clients require OAuth2.1 and Dynamic Client Registration (DCR).** This requirement exists because the MCP spec currently does not define a standard way for an MCP client to provide an OAuth `client_id`/`client_secret` from the user.

Here's the challenge DCR solves:
- **Traditional OAuth**: Requires pre-registered client credentials (`client_id`/`client_secret`)
- **MCP Context**: Users install MCP servers dynamically, without pre-coordination
- **DCR Solution**: Allows MCP clients to dynamically register themselves and obtain client credentials on-the-fly

The requirements for MCP OAuth can be found [here](https://modelcontextprotocol.io/specification/draft/basic/authorization#overview). Dynamic Client Registration (DCR) is typically the feature that most companies do not currently support, which is why **several companies with public-facing MCPs have implemented DCR into their OAuth capabilities** to enable the self-serve option.

### Registering your own OAuth Server

While this is still fairly unadopted, companies like [Stripe](https://docs.stripe.com/mcp), [Asana](https://developers.asana.com/docs/integrating-with-asanas-mcp-server) & more have started to support DCR in their OAuth flows to accommodate MCP. If you want to host an MCP server for large-scale use by external developers, you should plan to build out support for DCR in your API.

If your underlying API supports the necessary OAuth requirements, you can easily place any OAuth server in front of any Gram MCP Server with just a few clicks!

Only one OAuth flow can be placed in front of an MCP server, so it is very important that your MCP server only includes a single downstream API provider that takes in OAuth.

The artifact you are able to produce should look something like this:

```
{
  "issuer": "https://marketplace.stripe.com",
  "authorization_endpoint": "https://marketplace.stripe.com/oauth/v2/authorize",
  "token_endpoint": "https://marketplace.stripe.com/oauth/v2/token",
  "registration_endpoint": "https://marketplace.stripe.com/oauth/v2/register/tailorapp%2AAZfBZ6Q69QAAADJI%23EhcKFWFjY3RfMVJlaTA0QUo4QktoWGxzQw",
  "response_types_supported": [
    "code"
  ],
  "grant_types_supported": [
    "authorization_code",
    "refresh_token"
  ],
  "code_challenge_methods_supported": [
    "S256"
  ],
  "token_endpoint_auth_methods_supported": [
    "none"
  ]
}
```

Note: An MCP Client such as Claude will use the same client_id in perpetuity unless you explicitly provide a `client_secret_expires_at` value in your `/register` response. When implementing DCR, it is extremely important that you persist the client_ids you issue. MCP clients follow the OAuth specification precisely when it comes to retaining client_ids from DCR and they will not forget them when you uninstall a server!


### OAuth Proxy

For companies whose OAuth systems do not yet support the MCP requirements, Gram offers an OAuth proxy that translates between MCP requirements and standard OAuth implementations. **This is essentially a workaround to avoid implementing the full MCP requirements of DCR**, but it comes with important caveats.

How the OAuth proxy works:
- **Frontend**: Exposes OAuth 2.1 and DCR to MCP clients on your behalf
- **Backend**: Communicates with your OAuth APIs using a single set of credentials
- **Translation**: Handles the complexity of MCP-compliant OAuth flows

Functionally, this is very similar to solutions others might be familiar with, such as the [Cloudflare OAuth proxy](https://blog.cloudflare.com/remote-model-context-protocol-servers-mcp/#workers-oauth-provider-an-oauth-2-1-provider-library-for-cloudflare-workers).

## Important Caveats and Limitations

**Single Client Credentials**: Because your OAuth APIs require a specific `client_id`/`client_secret`, your MCP server stores a single set of these values to act as a shared client. This means:
- All users share the same underlying OAuth client
- You lose per-user client isolation
- Rate limiting and quotas apply to the shared client

**Suitability Considerations**: 
- ✅ **Good for**: Internal tools, controlled user bases, POCs and demos
- ❌ **May not be suitable for**: Public servers serving all your customers, scenarios requiring per-user client isolation
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

## Summary

When implementing authentication for your MCP server, remember:

1. **OAuth exchange is NOT required** - passing access tokens directly is completely valid
2. **Choose the right approach** for your use case:
   - Access tokens: Simple, works with existing systems
   - Client credentials: Good for server-to-server auth
   - User-facing OAuth: Best for public servers (requires DCR or proxy)
3. **DCR is only needed** if you want MCP clients to handle OAuth flows automatically
4. **OAuth proxy is available** as a workaround for DCR, but comes with limitations
5. **We can help** with white-glove service for proxy setup or DCR compliance

The goal is to choose the authentication method that works best with your existing infrastructure while providing the right user experience for your MCP server's intended use case.
