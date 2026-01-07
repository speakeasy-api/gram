<!-- Start SDK Example Usage [usage] -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.slack.slackLogin({
    projectSlug: "<value>",
  });

  console.log(result);
}

run();

```
<!-- End SDK Example Usage [usage] -->