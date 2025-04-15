# Assets
(*assets*)

## Overview

Manages assets used by Gram projects.

### Available Operations

* [uploadOpenAPIv3](#uploadopenapiv3) - uploadOpenAPIv3 assets

## uploadOpenAPIv3

Upload an OpenAPI v3 document to Gram.

### Example Usage

```typescript
import { GramAPI } from "@gram/sdk";
import { openAsBlob } from "node:fs";

const gramAPI = new GramAPI({
  security: {
    projectSlugHeaderGramProject: "<YOUR_API_KEY_HERE>",
    sessionHeaderGramSession: "<YOUR_API_KEY_HERE>",
  },
});

async function run() {
  const result = await gramAPI.assets.uploadOpenAPIv3({
    contentLength: 924456,
    requestBody: await openAsBlob("example.file"),
  });

  // Handle the result
  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramAPICore } from "@gram/sdk/core.js";
import { assetsUploadOpenAPIv3 } from "@gram/sdk/funcs/assetsUploadOpenAPIv3.js";
import { openAsBlob } from "node:fs";

// Use `GramAPICore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gramAPI = new GramAPICore({
  security: {
    projectSlugHeaderGramProject: "<YOUR_API_KEY_HERE>",
    sessionHeaderGramSession: "<YOUR_API_KEY_HERE>",
  },
});

async function run() {
  const res = await assetsUploadOpenAPIv3(gramAPI, {
    contentLength: 924456,
    requestBody: await openAsBlob("example.file"),
  });

  if (!res.ok) {
    throw res.error;
  }

  const { value: result } = res;

  // Handle the result
  console.log(result);
}

run();
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.AssetsNumberUploadOpenAPIv3Request](../../models/operations/assetsnumberuploadopenapiv3request.md)                                                                 | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.UploadOpenAPIv3Result](../../models/components/uploadopenapiv3result.md)\>**

### Errors

| Error Type      | Status Code     | Content Type    |
| --------------- | --------------- | --------------- |
| errors.APIError | 4XX, 5XX        | \*/\*           |