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

| Field         | Type     | Required           | Description                                                              |
| ------------- | -------- | ------------------ | ------------------------------------------------------------------------ |
| `policyId`    | _string_ | :heavy_check_mark: | The risk policy ID.                                                      |
| `match`       | _string_ | :heavy_check_mark: | The MCP server identifier to revoke — exactly the value used to approve. |
| `gramKey`     | _string_ | :heavy_minus_sign: | API Key header                                                           |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header                                                           |
| `gramProject` | _string_ | :heavy_minus_sign: | project header                                                           |
