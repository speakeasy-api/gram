# ExclusionScope

The scope used to store exception rules for this scope.

## Example Usage

```typescript
import { ExclusionScope } from "@gram/client/models/components/scopedefinition.js";

let value: ExclusionScope = "environment:blocked_write";
```

## Values

```typescript
"org:blocked_read" | "org:blocked_admin" | "project:blocked_read" | "project:blocked_write" | "mcp:blocked_read" | "mcp:blocked_write" | "mcp:blocked_connect" | "environment:blocked_read" | "environment:blocked_write" | "risk_policy:bypass"
```