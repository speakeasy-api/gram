# Assets
(*assets*)

## Overview

Manages assets used by Gram projects.

### Available Operations

* [assetsNumberUploadOpenAPIv3](#assetsnumberuploadopenapiv3) - uploadOpenAPIv3 assets

## assetsNumberUploadOpenAPIv3

Upload an OpenAPI v3 document to Gram.

### Example Usage

```typescript
import { Gram } from "@gram/sdk";

const gram = new Gram({
  sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"] ?? "",
});

async function run() {
  const result = await gram.assets.assetsNumberUploadOpenAPIv3({
    contentLength: 7818005087342603000,
    gramProject: "Consequuntur non dolor iure dolor iste voluptas.",
    gramSession: "Qui est quas veritatis rerum et.",
  });

  // Handle the result
  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/sdk/core.js";
import { assetsAssetsNumberUploadOpenAPIv3 } from "@gram/sdk/funcs/assetsAssetsNumberUploadOpenAPIv3.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore({
  sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"] ?? "",
});

async function run() {
  const res = await assetsAssetsNumberUploadOpenAPIv3(gram, {
    contentLength: 7818005087342603000,
    gramProject: "Consequuntur non dolor iure dolor iste voluptas.",
    gramSession: "Qui est quas veritatis rerum et.",
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

### React hooks and utilities

This method can be used in React components through the following hooks and
associated utilities.

> Check out [this guide][hook-guide] for information about each of the utilities
> below and how to get started using React hooks.

[hook-guide]: ../../../REACT_QUERY.md

```tsx
import {
  // Mutation hook for triggering the API call.
  useUploadOpenAPIv3Mutation
} from "@gram/sdk/react-query/assetsAssetsNumberUploadOpenAPIv3.js";
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