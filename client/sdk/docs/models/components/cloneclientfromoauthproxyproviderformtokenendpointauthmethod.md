# CloneClientFromOAuthProxyProviderFormTokenEndpointAuthMethod

How the cloned client authenticates at the issuer's token endpoint. Omit to default to client_secret_basic.

## Example Usage

```typescript
import { CloneClientFromOAuthProxyProviderFormTokenEndpointAuthMethod } from "@gram/client/models/components/cloneclientfromoauthproxyproviderform.js";

let value: CloneClientFromOAuthProxyProviderFormTokenEndpointAuthMethod =
  "none";
```

## Values

```typescript
"client_secret_basic" | "client_secret_post" | "none";
```
