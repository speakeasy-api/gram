# EvolveDeploymentRequest

## Example Usage

```typescript
import { EvolveDeploymentRequest } from "@gram/client/models/operations/evolvedeployment.js";

let value: EvolveDeploymentRequest = {
  evolveForm: {
    nonBlocking: false,
    upsertExternalMcps: [
      {
        name: "My Slack Integration",
        registryServerSpecifier: "slack",
        selectedRemotes: ["https://mcp.example.com/sse"],
        slug: "<value>",
      },
    ],
  },
};
```

## Fields

| Field         | Type                                                           | Required           | Description    |
| ------------- | -------------------------------------------------------------- | ------------------ | -------------- |
| `gramKey`     | _string_                                                       | :heavy_minus_sign: | API Key header |
| `gramSession` | _string_                                                       | :heavy_minus_sign: | Session header |
| `gramProject` | _string_                                                       | :heavy_minus_sign: | project header |
| `evolveForm`  | [components.EvolveForm](../../models/components/evolveform.md) | :heavy_check_mark: | N/A            |
