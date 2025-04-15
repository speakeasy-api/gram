<!-- Start SDK Example Usage [usage] -->
```typescript
import { SDK } from "@gram/sdk";
import { openAsBlob } from "node:fs";

const sdk = new SDK({
  security: {
    projectSlugHeaderGramProject: "<YOUR_API_KEY_HERE>",
    sessionHeaderGramSession: "<YOUR_API_KEY_HERE>",
  },
});

async function run() {
  const result = await sdk.assets.uploadOpenAPIv3({
    contentLength: 924456,
    requestBody: await openAsBlob("example.file"),
  });

  // Handle the result
  console.log(result);
}

run();

```
<!-- End SDK Example Usage [usage] -->