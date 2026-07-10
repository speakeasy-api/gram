# Scope

The scope slug this grant applies to.

## Example Usage

```typescript
import { Scope } from "@gram/client/models/components/rolegrant.js";

let value: Scope = "org:admin";
```

## Values

```typescript
"org:read" | "org:blocked_read" | "org:admin" | "org:blocked_admin" | "project:read" | "project:blocked_read" | "project:write" | "project:blocked_write" | "mcp:read" | "mcp:blocked_read" | "mcp:write" | "mcp:blocked_write" | "mcp:connect" | "mcp:blocked_connect" | "environment:read" | "environment:blocked_read" | "environment:write" | "environment:blocked_write" | "risk_policy:evaluate" | "risk_policy:bypass" | "chat:read"
```