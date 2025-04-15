# Keys
(*keys*)

## Overview

Managing system api keys.

### Available Operations

* [create](#create) - createKey keys
* [list](#list) - listKeys keys
* [revokeById](#revokebyid) - revokeKey keys

## create

Create a new api key

### Example Usage

```typescript
import { GramAPI } from "@gram/sdk";

const gramAPI = new GramAPI();

async function run() {
  const result = await gramAPI.keys.create({
    sessionHeaderGramSession: "<YOUR_API_KEY_HERE>",
  }, {
    createKeyForm: {
      name: "<value>",
    },
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
import { keysCreate } from "@gram/sdk/funcs/keysCreate.js";

// Use `GramAPICore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gramAPI = new GramAPICore();

async function run() {
  const res = await keysCreate(gramAPI, {
    sessionHeaderGramSession: "<YOUR_API_KEY_HERE>",
  }, {
    createKeyForm: {
      name: "<value>",
    },
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
| `request`                                                                                                                                                                      | [operations.KeysNumberCreateKeyRequest](../../models/operations/keysnumbercreatekeyrequest.md)                                                                                 | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.KeysNumberCreateKeySecurity](../../models/operations/keysnumbercreatekeysecurity.md)                                                                               | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.Key](../../models/components/key.md)\>**

### Errors

| Error Type      | Status Code     | Content Type    |
| --------------- | --------------- | --------------- |
| errors.APIError | 4XX, 5XX        | \*/\*           |

## list

List all api keys for an organization

### Example Usage

```typescript
import { GramAPI } from "@gram/sdk";

const gramAPI = new GramAPI();

async function run() {
  const result = await gramAPI.keys.list({
    sessionHeaderGramSession: "<YOUR_API_KEY_HERE>",
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
import { keysList } from "@gram/sdk/funcs/keysList.js";

// Use `GramAPICore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gramAPI = new GramAPICore();

async function run() {
  const res = await keysList(gramAPI, {
    sessionHeaderGramSession: "<YOUR_API_KEY_HERE>",
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
| `request`                                                                                                                                                                      | [operations.KeysNumberListKeysRequest](../../models/operations/keysnumberlistkeysrequest.md)                                                                                   | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.KeysNumberListKeysSecurity](../../models/operations/keysnumberlistkeyssecurity.md)                                                                                 | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.ListKeysResult](../../models/components/listkeysresult.md)\>**

### Errors

| Error Type      | Status Code     | Content Type    |
| --------------- | --------------- | --------------- |
| errors.APIError | 4XX, 5XX        | \*/\*           |

## revokeById

Revoke a api key

### Example Usage

```typescript
import { GramAPI } from "@gram/sdk";

const gramAPI = new GramAPI();

async function run() {
  await gramAPI.keys.revokeById({
    sessionHeaderGramSession: "<YOUR_API_KEY_HERE>",
  }, {
    id: "<id>",
  });


}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramAPICore } from "@gram/sdk/core.js";
import { keysRevokeById } from "@gram/sdk/funcs/keysRevokeById.js";

// Use `GramAPICore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gramAPI = new GramAPICore();

async function run() {
  const res = await keysRevokeById(gramAPI, {
    sessionHeaderGramSession: "<YOUR_API_KEY_HERE>",
  }, {
    id: "<id>",
  });

  if (!res.ok) {
    throw res.error;
  }

  const { value: result } = res;

  
}

run();
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.KeysNumberRevokeKeyRequest](../../models/operations/keysnumberrevokekeyrequest.md)                                                                                 | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.KeysNumberRevokeKeySecurity](../../models/operations/keysnumberrevokekeysecurity.md)                                                                               | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<void\>**

### Errors

| Error Type      | Status Code     | Content Type    |
| --------------- | --------------- | --------------- |
| errors.APIError | 4XX, 5XX        | \*/\*           |