# Instances
(*instances*)

## Overview

Consumer APIs for interacting with all relevant data for an instance of a toolset and environment.

### Available Operations

* [getBySlug](#getbyslug) - getInstance instances

## getBySlug

Load all relevant data for an instance of a toolset and environment

### Example Usage

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

### Standalone function

The standalone function version of this method:

```typescript
import { GramAPICore } from "@gram-ai/sdk/core.js";
import { instancesGetBySlug } from "@gram-ai/sdk/funcs/instancesGetBySlug.js";

// Use `GramAPICore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gramAPI = new GramAPICore();

async function run() {
  const res = await instancesGetBySlug(gramAPI, {
    option1: {
      projectSlugHeaderGramProject: "<YOUR_API_KEY_HERE>",
      sessionHeaderGramSession: "<YOUR_API_KEY_HERE>",
    },
  }, {
    toolsetSlug: "<value>",
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
| `request`                                                                                                                                                                      | [operations.GetInstanceRequest](../../models/operations/getinstancerequest.md)                                                                                                 | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.GetInstanceSecurity](../../models/operations/getinstancesecurity.md)                                                                                               | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.GetInstanceResult](../../models/components/getinstanceresult.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500                               | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |