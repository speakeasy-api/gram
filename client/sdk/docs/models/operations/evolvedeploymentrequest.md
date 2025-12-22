# EvolveDeploymentRequest

## Example Usage

```typescript
import { EvolveDeploymentRequest } from "@gram/client/models/operations";

let value: EvolveDeploymentRequest = {
  evolveForm: {
    nonBlocking: false,
    upsertExternalMcps: [
      {
        name: "My Slack Integration",
        registryId: "f1d0bf1d-70c9-4a3a-9de8-8ddb27c0c81b",
        registryServerSpecifier: "slack",
        slug: "<value>",
      },
    ],
  },
};
```

## Fields

| Field                                                          | Type                                                           | Required                                                       | Description                                                    |
| -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- |
| `gramKey`                                                      | *string*                                                       | :heavy_minus_sign:                                             | API Key header                                                 |
| `gramSession`                                                  | *string*                                                       | :heavy_minus_sign:                                             | Session header                                                 |
| `gramProject`                                                  | *string*                                                       | :heavy_minus_sign:                                             | project header                                                 |
| `evolveForm`                                                   | [components.EvolveForm](../../models/components/evolveform.md) | :heavy_check_mark:                                             | N/A                                                            |