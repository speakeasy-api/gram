# ListShadowMCPApprovalRequestsRequest

## Example Usage

```typescript
import { ListShadowMCPApprovalRequestsRequest } from "@gram/client/models/operations/listshadowmcpapprovalrequests.js";

let value: ListShadowMCPApprovalRequestsRequest = {};
```

## Fields

| Field         | Type                                                   | Required           | Description                          |
| ------------- | ------------------------------------------------------ | ------------------ | ------------------------------------ |
| `status`      | [operations.Status](../../models/operations/status.md) | :heavy_minus_sign: | N/A                                  |
| `projectId`   | _string_                                               | :heavy_minus_sign: | N/A                                  |
| `limit`       | _number_                                               | :heavy_minus_sign: | N/A                                  |
| `cursor`      | _string_                                               | :heavy_minus_sign: | Cursor for the next page of results. |
| `gramSession` | _string_                                               | :heavy_minus_sign: | Session header                       |
