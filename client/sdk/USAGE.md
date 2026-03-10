<!-- Start SDK Example Usage [usage] -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.assets.createSignedChatAttachmentURL({
    createSignedChatAttachmentURLForm2: {
      id: "<id>",
      projectId: "<id>",
    },
  });

  console.log(result);
}

run();

```
<!-- End SDK Example Usage [usage] -->