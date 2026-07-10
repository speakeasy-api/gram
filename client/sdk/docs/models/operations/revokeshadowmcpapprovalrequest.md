# RevokeShadowMCPApprovalRequest

## Example Usage

```typescript
import { RevokeShadowMCPApprovalRequest } from "@gram/client/models/operations";

let value: RevokeShadowMCPApprovalRequest = {
  policyId: "953bb6db-0bbb-40fc-baa3-083040471d77",
  match: "<value>",
};
```

## Fields

| Field                                                                    | Type                                                                     | Required                                                                 | Description                                                              |
| ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ |
| `policyId`                                                               | *string*                                                                 | :heavy_check_mark:                                                       | The risk policy ID.                                                      |
| `match`                                                                  | *string*                                                                 | :heavy_check_mark:                                                       | The MCP server identifier to revoke — exactly the value used to approve. |
| `gramKey`                                                                | *string*                                                                 | :heavy_minus_sign:                                                       | API Key header                                                           |
| `gramSession`                                                            | *string*                                                                 | :heavy_minus_sign:                                                       | Session header                                                           |
| `gramProject`                                                            | *string*                                                                 | :heavy_minus_sign:                                                       | project header                                                           |