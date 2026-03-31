<!-- Start SDK Example Usage [usage] -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.access.list();

  console.log(result);
}

run();

```
<!-- End SDK Example Usage [usage] -->