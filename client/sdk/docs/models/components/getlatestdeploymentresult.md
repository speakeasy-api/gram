# GetLatestDeploymentResult

## Example Usage

```typescript
import { GetLatestDeploymentResult } from "@gram/client/models/components";

let value: GetLatestDeploymentResult = {
  deployment: {
    clonedFrom: "bc5f4a555e933e6861d12edba4c2d87ef6caf8e6",
    createdAt: new Date("2025-05-24T05:11:37.963Z"),
    externalId: "bc5f4a555e933e6861d12edba4c2d87ef6caf8e6",
    functionsToolCount: 643083,
    githubPr: "1234",
    githubRepo: "speakeasyapi/gram",
    githubSha: "f33e693e9e12552043bc0ec5c37f1b8a9e076161",
    id: "bc5f4a555e933e6861d12edba4c2d87ef6caf8e6",
    idempotencyKey: "01jqq0ajmb4qh9eppz48dejr2m",
    openapiv3Assets: [
      {
        assetId: "<id>",
        id: "<id>",
        name: "<value>",
        slug: "<value>",
      },
    ],
    openapiv3ToolCount: 325382,
    organizationId: "<id>",
    packages: [
      {
        id: "<id>",
        name: "<value>",
        version: "<value>",
      },
    ],
    projectId: "<id>",
    status: "<value>",
    userId: "<id>",
  },
};
```

## Fields

| Field                                                          | Type                                                           | Required                                                       | Description                                                    |
| -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- |
| `deployment`                                                   | [components.Deployment](../../models/components/deployment.md) | :heavy_minus_sign:                                             | N/A                                                            |