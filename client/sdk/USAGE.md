<!-- Start SDK Example Usage [usage] -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  await gram.access.delete({
    id: "8b5418db-b219-4749-bea3-c98a31530d70",
  });
}

run();

```
<!-- End SDK Example Usage [usage] -->