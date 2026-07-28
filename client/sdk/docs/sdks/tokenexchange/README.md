# TokenExchange

## Overview

Device-agent token exchange: trade an org-scoped install credential (an API key with the 'agent' scope) plus a vouched user email for a long-lived, per-user API key scoped for the device agent.

### Available Operations

- [exchange](#exchange) - exchange tokenExchange

## exchange

Exchange the org-scoped device-agent install credential plus a vouched user email for a long-lived, per-user API key carrying the 'agent_user' scope. Authenticated with an org-scoped API key carrying the 'agent' scope — deliberately broader than the 'agent_user' scope the minted per-user keys carry, so a leaked per-user key cannot re-enter this endpoint to forge another user's key. The raw key is returned exactly once.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="tokenExchange" method="post" path="/rpc/tokenExchange.exchange" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.tokenExchange.exchange({
    exchangeRequestBody: {
      email: "dev@acme.corp",
    },
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { tokenExchangeExchange } from "@gram/client/funcs/tokenExchangeExchange.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await tokenExchangeExchange(gram, {
    exchangeRequestBody: {
      email: "dev@acme.corp",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("tokenExchangeExchange failed:", res.error);
  }
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
  useTokenExchangeExchangeMutation,
} from "@gram/client/react-query/tokenExchangeExchange.js";
```

### Parameters

| Parameter              | Type                                                                                    | Required           | Description                                                                                                                                                                    |
| ---------------------- | --------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.TokenExchangeRequest](../../models/operations/tokenexchangerequest.md)      | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.TokenExchangeSecurity](../../models/operations/tokenexchangesecurity.md)    | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                          | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options) | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                           | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.TokenResult](../../models/components/tokenresult.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |
