# Risk.Evals

## Overview

### Available Operations

* [deleteReview](#deletereview) - deleteRiskEvalReview risk
* [evaluate](#evaluate) - evaluatePromptGuardrail risk
* [listReviews](#listreviews) - listRiskEvalReviews risk
* [saveReview](#savereview) - saveRiskEvalReview risk

## deleteReview

Remove the current reviewer's verdict for one session (the toggle-off path). A reviewer can only clear their own verdict.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="deleteRiskEvalReview" method="delete" path="/rpc/riskEvals.deleteReview" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  await gram.risk.evals.deleteReview({
    policyId: "7d33d4a1-10e2-4676-8f01-7b6bb7cc1d94",
    chatId: "9f1f3c19-77e1-47bc-8648-eacff83771bc",
  });


}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { riskEvalsDeleteReview } from "@gram/client/funcs/riskEvalsDeleteReview.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await riskEvalsDeleteReview(gram, {
    policyId: "7d33d4a1-10e2-4676-8f01-7b6bb7cc1d94",
    chatId: "9f1f3c19-77e1-47bc-8648-eacff83771bc",
  });
  if (res.ok) {
    const { value: result } = res;
    
  } else {
    console.log("riskEvalsDeleteReview failed:", res.error);
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
  useRiskDeleteEvalReviewMutation
} from "@gram/client/react-query/riskEvalsDeleteReview.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.DeleteRiskEvalReviewRequest](../../models/operations/deleteriskevalreviewrequest.md)                                                                               | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.DeleteRiskEvalReviewSecurity](../../models/operations/deleteriskevalreviewsecurity.md)                                                                             | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<void\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |

## evaluate

Replay a prompt_based guardrail against a single chat session and return the LLM judge's per-message verdict. The guardrail (prompt + judge config + message-type scope + CEL scope) is passed inline so the policy-eval workbench can evaluate an unsaved draft before a policy exists. This path is read-only: it never writes risk_results, publishes to the outbox, or enforces. It exists purely to tune a guardrail against real transcripts. Judges only the chat's latest generation; message-type scoping and CEL scope predicates are both applied.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="evaluatePromptGuardrail" method="post" path="/rpc/riskEvals.evaluate" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.risk.evals.evaluate({
    evaluatePromptGuardrailRequestBody: {
      chatId: "e4912c42-9ca6-46fd-8057-460714cea6b8",
      prompt: "<value>",
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
import { riskEvalsEvaluate } from "@gram/client/funcs/riskEvalsEvaluate.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await riskEvalsEvaluate(gram, {
    evaluatePromptGuardrailRequestBody: {
      chatId: "e4912c42-9ca6-46fd-8057-460714cea6b8",
      prompt: "<value>",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("riskEvalsEvaluate failed:", res.error);
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
  // Query hooks for fetching data.
  useRiskEvaluatePromptGuardrail,
  useRiskEvaluatePromptGuardrailSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchRiskEvaluatePromptGuardrail,
  
  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateRiskEvaluatePromptGuardrail,
  invalidateAllRiskEvaluatePromptGuardrail,
} from "@gram/client/react-query/riskEvalsEvaluate.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.EvaluatePromptGuardrailRequest](../../models/operations/evaluatepromptguardrailrequest.md)                                                                         | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.EvaluatePromptGuardrailSecurity](../../models/operations/evaluatepromptguardrailsecurity.md)                                                                       | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.PromptGuardrailEvalResult](../../models/components/promptguardrailevalresult.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |

## listReviews

List the active regression set for a prompt-based policy: every reviewer's current ground-truth verdicts.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="listRiskEvalReviews" method="get" path="/rpc/riskEvals.listReviews" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.risk.evals.listReviews({
    policyId: "12c1410e-6f7a-4401-ba26-030e46dc41b9",
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { riskEvalsListReviews } from "@gram/client/funcs/riskEvalsListReviews.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await riskEvalsListReviews(gram, {
    policyId: "12c1410e-6f7a-4401-ba26-030e46dc41b9",
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("riskEvalsListReviews failed:", res.error);
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
  // Query hooks for fetching data.
  useRiskListEvalReviews,
  useRiskListEvalReviewsSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchRiskListEvalReviews,
  
  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateRiskListEvalReviews,
  invalidateAllRiskListEvalReviews,
} from "@gram/client/react-query/riskEvalsListReviews.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.ListRiskEvalReviewsRequest](../../models/operations/listriskevalreviewsrequest.md)                                                                                 | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.ListRiskEvalReviewsSecurity](../../models/operations/listriskevalreviewssecurity.md)                                                                               | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.ListRiskEvalReviewsResult](../../models/components/listriskevalreviewsresult.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |

## saveReview

Record (or replace) the current reviewer's ground-truth verdict for one chat session under a prompt-based policy. This is the durable regression set the eval workbench scores the live guardrail against. Upserts: a reviewer has at most one verdict per session per policy.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="saveRiskEvalReview" method="post" path="/rpc/riskEvals.saveReview" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.risk.evals.saveReview({
    saveRiskEvalReviewRequestBody: {
      chatId: "fb145521-1e78-4d01-bc1a-c91ba7508a75",
      policyId: "d33108fc-9369-4d3c-aa28-b0dd07372081",
      verdict: "missed",
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
import { riskEvalsSaveReview } from "@gram/client/funcs/riskEvalsSaveReview.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await riskEvalsSaveReview(gram, {
    saveRiskEvalReviewRequestBody: {
      chatId: "fb145521-1e78-4d01-bc1a-c91ba7508a75",
      policyId: "d33108fc-9369-4d3c-aa28-b0dd07372081",
      verdict: "missed",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("riskEvalsSaveReview failed:", res.error);
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
  useRiskSaveEvalReviewMutation
} from "@gram/client/react-query/riskEvalsSaveReview.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.SaveRiskEvalReviewRequest](../../models/operations/saveriskevalreviewrequest.md)                                                                                   | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.SaveRiskEvalReviewSecurity](../../models/operations/saveriskevalreviewsecurity.md)                                                                                 | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.RiskPolicyEvalReview](../../models/components/riskpolicyevalreview.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |