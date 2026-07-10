# ApproveShadowMCPRequestBody

## Example Usage

```typescript
import { ApproveShadowMCPRequestBody } from "@gram/client/models/components";

let value: ApproveShadowMCPRequestBody = {
  match: "<value>",
  policyId: "8ebed26e-92a5-4347-823c-0ff4fe6017ba",
};
```

## Fields

| Field                                              | Type                                               | Required                                           | Description                                        |
| -------------------------------------------------- | -------------------------------------------------- | -------------------------------------------------- | -------------------------------------------------- |
| `match`                                            | *string*                                           | :heavy_check_mark:                                 | The MCP server identifier to approve.              |
| `policyId`                                         | *string*                                           | :heavy_check_mark:                                 | The risk policy ID.                                |
| `serverName`                                       | *string*                                           | :heavy_minus_sign:                                 | Display name of the MCP server (optional, for UI). |