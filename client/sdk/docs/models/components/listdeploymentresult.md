# ListDeploymentResult

## Example Usage

```typescript
import { ListDeploymentResult } from "@gram/client/models/components";

let value: ListDeploymentResult = {
  items: [
    {
      createdAt: new Date("2026-12-13T12:32:14.714Z"),
      functionsAssetCount: 628707,
      functionsToolCount: 665973,
      id: "bc5f4a555e933e6861d12edba4c2d87ef6caf8e6",
      openapiv3AssetCount: 423109,
      openapiv3ToolCount: 373309,
      status: "<value>",
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