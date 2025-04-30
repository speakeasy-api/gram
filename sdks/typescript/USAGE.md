<!-- Start SDK Example Usage [usage] -->
```typescript
import { GramAPI } from "@gram-ai/sdk";

const gramAPI = new GramAPI();

async function run() {
  const result = await gramAPI.instances.getBySlug({
    option1: {
      projectSlugHeaderGramProject: "<YOUR_API_KEY_HERE>",
      sessionHeaderGramSession: "<YOUR_API_KEY_HERE>",
    },
  }, {
    toolsetSlug: "<value>",
  });

  // Handle the result
  console.log(result);
}

run();

```
<!-- End SDK Example Usage [usage] -->