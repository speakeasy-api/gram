# Risk.Rules

## Overview

### Available Operations

- [test](#test) - testDetectionRule risk

## test

Run a single detection rule against pasted sample text and return any matches. Reuses the same scanner code (gitleaks, Presidio, prompt-injection, custom regex) that the analyzer runs in production so the playground match shape mirrors the chat-message path.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="testDetectionRule" method="post" path="/rpc/risk.testRule" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.risk.rules.test({
    testDetectionRuleRequestBody: {
      ruleId: "<id>",
      text: "<value>",
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
import { riskRulesTest } from "@gram/client/funcs/riskRulesTest.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await riskRulesTest(gram, {
    testDetectionRuleRequestBody: {
      ruleId: "<id>",
      text: "<value>",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("riskRulesTest failed:", res.error);
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
  useRiskTestDetectionRuleMutation,
} from "@gram/client/react-query/riskRulesTest.js";
```

### Parameters

| Parameter              | Type                                                                                         | Required           | Description                                                                                                                                                                    |
| ---------------------- | -------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.TestDetectionRuleRequest](../../models/operations/testdetectionrulerequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.TestDetectionRuleSecurity](../../models/operations/testdetectionrulesecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                               | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)      | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.TestDetectionRuleResult](../../models/components/testdetectionruleresult.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |
