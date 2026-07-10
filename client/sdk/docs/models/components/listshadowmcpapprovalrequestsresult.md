# ListShadowMCPApprovalRequestsResult

## Example Usage

```typescript
import { ListShadowMCPApprovalRequestsResult } from "@gram/client/models/components/listshadowmcpapprovalrequestsresult.js";

let value: ListShadowMCPApprovalRequestsResult = {
  requests: [
    {
      blockedCount: 908763,
      createdAt: new Date("2026-03-29T02:22:32.981Z"),
      id: "e3507c95-5591-4340-b445-758d726431ae",
      organizationId: "<id>",
      projectId: "fde54024-e0b3-429d-80c2-79769c69b805",
      requestedAt: new Date("2025-08-11T06:31:32.403Z"),
      resourceType: "<value>",
      status: "requested",
      updatedAt: new Date("2024-09-25T04:19:04.375Z"),
    },
  ],
};
```

## Fields

| Field        | Type                                                                                         | Required           | Description                          |
| ------------ | -------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------ |
| `nextCursor` | _string_                                                                                     | :heavy_minus_sign: | Cursor for the next page of results. |
| `requests`   | [components.ShadowMCPApprovalRequest](../../models/components/shadowmcpapprovalrequest.md)[] | :heavy_check_mark: | N/A                                  |
