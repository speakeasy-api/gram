<!-- Start SDK Example Usage [usage] -->
```typescript
import { Gram } from "@gram/sdk";

const gram = new Gram({
  sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"]
    ?? "",
});

async function run() {
  const result = await gram.system.systemNumberHealthCheck();

  // Handle the result
  console.log(result);
}

run();

```
<!-- End SDK Example Usage [usage] -->