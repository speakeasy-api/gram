# CreateDeploymentRequest

## Example Usage

```typescript
import { CreateDeploymentRequest } from "@gram/client/models/operations";

let value: CreateDeploymentRequest = {
  idempotencyKey: "01jqq0ajmb4qh9eppz48dejr2m",
  createDeploymentRequestBody: {
    externalId: "bc5f4a555e933e6861d12edba4c2d87ef6caf8e6",
    externalMcps: [
      {
        name: "My Slack Integration",
        registryId: "dc2de89f-c791-4883-835b-59b1b715de3a",
        registryServerSpecifier: "slack",
        slug: "<value>",
        userAgent: "MyApp/1.0",
      },
    ],
    githubPr: "1234",
    githubRepo: "speakeasyapi/gram",
    githubSha: "f33e693e9e12552043bc0ec5c37f1b8a9e076161",
    nonBlocking: false,
  },
};
```

## Fields

| Field                                                                                            | Type                                                                                             | Required                                                                                         | Description                                                                                      | Example                                                                                          |
| ------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------ |
| `gramKey`                                                                                        | *string*                                                                                         | :heavy_minus_sign:                                                                               | API Key header                                                                                   |                                                                                                  |
| `gramSession`                                                                                    | *string*                                                                                         | :heavy_minus_sign:                                                                               | Session header                                                                                   |                                                                                                  |
| `gramProject`                                                                                    | *string*                                                                                         | :heavy_minus_sign:                                                                               | project header                                                                                   |                                                                                                  |
| `idempotencyKey`                                                                                 | *string*                                                                                         | :heavy_check_mark:                                                                               | A unique identifier that will mitigate against duplicate deployments.                            | 01jqq0ajmb4qh9eppz48dejr2m                                                                       |
| `createDeploymentRequestBody`                                                                    | [components.CreateDeploymentRequestBody](../../models/components/createdeploymentrequestbody.md) | :heavy_check_mark:                                                                               | N/A                                                                                              |                                                                                                  |