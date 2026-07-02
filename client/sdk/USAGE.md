<!-- Start SDK Example Usage [usage] -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.access.approveShadowMCPApprovalRequest({
    approveShadowMCPApprovalRequestForm: {
      accessScope: "organization",
      displayName: "Danny75",
      id: "07e9687f-f01e-43f5-9a11-0ddd8f277af1",
      matchBreadth: "full_url",
      matchValue: "<value>",
    },
  });

  console.log(result);
}

run();

```
<!-- End SDK Example Usage [usage] -->