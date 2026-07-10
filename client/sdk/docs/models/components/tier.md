# Tier

Graph tier. Origin nodes identify the hostname or client context that started the call, not the MCP server URL.

## Example Usage

```typescript
import { Tier } from "@gram/client/models/components/employeedataflownode.js";

let value: Tier = "client";
```

## Values

```typescript
"origin" | "client" | "server" | "tool";
```
