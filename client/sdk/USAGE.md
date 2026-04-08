<!-- Start SDK Example Usage [usage] -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.access.createRole({
    createRoleForm: {
      description: "swerve hm receptor how",
      grants: [
        {
          scope: "mcp:connect",
        },
      ],
      name: "<value>",
    },
  });

  console.log(result);
}

run();

```
<!-- End SDK Example Usage [usage] -->