# External

## Overview

Endpoints for external services to interact with gram.

### Available Operations

- [receiveWorkOSWebhook](#receiveworkoswebhook) - receiveWorkOSWebhook external

## receiveWorkOSWebhook

Receive and enqueue a WorkOS webhook event.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="receiveWorkOSWebhook" method="post" path="/rpc/external.receiveWorkOSWebhook" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  await gram.external.receiveWorkOSWebhook();
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { externalReceiveWorkOSWebhook } from "@gram/client/funcs/externalReceiveWorkOSWebhook.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await externalReceiveWorkOSWebhook(gram);
  if (res.ok) {
    const { value: result } = res;
  } else {
    console.log("externalReceiveWorkOSWebhook failed:", res.error);
  }
}

run();
```

### Parameters

| Parameter              | Type                                                                                             | Required           | Description                                                                                                                                                                    |
| ---------------------- | ------------------------------------------------------------------------------------------------ | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.ReceiveWorkOSWebhookRequest](../../models/operations/receiveworkoswebhookrequest.md) | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `options`              | RequestOptions                                                                                   | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)          | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                    | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<void\>**

### Errors

| Error Type      | Status Code | Content Type |
| --------------- | ----------- | ------------ |
| errors.APIError | 4XX, 5XX    | \*/\*        |
