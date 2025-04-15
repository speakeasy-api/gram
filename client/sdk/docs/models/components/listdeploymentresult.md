# ListDeploymentResult

## Example Usage

```typescript
import { ListDeploymentResult } from "@gram/client/models/components";

let value: ListDeploymentResult = {
  items: [
    {
      assetCount: 711355,
      createdAt: new Date("2025-12-13T12:32:14.714Z"),
      id: "bc5f4a555e933e6861d12edba4c2d87ef6caf8e6",
      userId: "<id>",
    },
  ],
  nextCursor: "01jp3f054qc02gbcmpp0qmyzed",
};
```

## Fields

| Field                                                                          | Type                                                                           | Required                                                                       | Description                                                                    | Example                                                                        |
| ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ |
| `items`                                                                        | [components.DeploymentSummary](../../models/components/deploymentsummary.md)[] | :heavy_check_mark:                                                             | A list of deployments                                                          |                                                                                |
| `nextCursor`                                                                   | *string*                                                                       | :heavy_minus_sign:                                                             | The cursor to fetch results from                                               | 01jp3f054qc02gbcmpp0qmyzed                                                     |