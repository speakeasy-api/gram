<!-- Start SDK Example Usage [usage] -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.mcp.mcpNumberServe({
    project: "<value>",
    toolset: "<value>",
    environment: "<value>",
  });

  // Handle the result
  console.log(result);
}

run();

```
<!-- End SDK Example Usage [usage] -->