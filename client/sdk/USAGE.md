<!-- Start SDK Example Usage [usage] -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram({
  security: {
    projectSlugHeaderGramProject:
      process.env["GRAM_PROJECT_SLUG_HEADER_GRAM_PROJECT"] ?? "",
    sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"]
      ?? "",
  },
});

async function run() {
  const result = await gram.assets.uploadOpenAPIv3({
    contentLength: 342044,
  });

  // Handle the result
  console.log(result);
}

run();

```
<!-- End SDK Example Usage [usage] -->