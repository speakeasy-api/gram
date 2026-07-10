# RiskBlock

## Example Usage

```typescript
import { RiskBlock } from "@gram/client/models/components/riskblock.js";

let value: RiskBlock = {
  createdAt: new Date("2024-09-03T09:03:18.467Z"),
  id: "65c7c47d-c373-4d04-9a48-718620843697",
  policyName: "<value>",
  projectId: "01a512b1-cebb-4b45-b6db-d625e9a9b40d",
  reason: "<value>",
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the block occurred.                                                                      |
| `feedback`                                                                                    | [components.RiskBlockFeedback](../../models/components/riskblockfeedback.md)                  | :heavy_minus_sign:                                                                            | Existing feedback sentiment recorded for this block, when any.                                |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The block ID (the underlying risk result ID).                                                 |
| `policyName`                                                                                  | *string*                                                                                      | :heavy_check_mark:                                                                            | Name of the risk policy that blocked the call.                                                |
| `projectId`                                                                                   | *string*                                                                                      | :heavy_check_mark:                                                                            | The project the block belongs to.                                                             |
| `reason`                                                                                      | *string*                                                                                      | :heavy_check_mark:                                                                            | Human-readable reason the tool call was blocked.                                              |
| `toolName`                                                                                    | *string*                                                                                      | :heavy_minus_sign:                                                                            | Name of the tool that was blocked, when known.                                                |