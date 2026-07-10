# SearchUsersPayload

Payload for searching user usage summaries

## Example Usage

```typescript
import { SearchUsersPayload } from "@gram/client/models/components/searchuserspayload.js";

let value: SearchUsersPayload = {
  filter: {
    from: new Date("2025-12-19T10:00:00Z"),
    to: new Date("2025-12-19T11:00:00Z"),
  },
  userType: "external",
};
```

## Fields

| Field                                                                                          | Type                                                                                           | Required                                                                                       | Description                                                                                    |
| ---------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| `cursor`                                                                                       | *string*                                                                                       | :heavy_minus_sign:                                                                             | Cursor for pagination (user identifier from last item)                                         |
| `filter`                                                                                       | [components.SearchUsersFilter](../../models/components/searchusersfilter.md)                   | :heavy_check_mark:                                                                             | Filter criteria for searching user usage summaries                                             |
| `groupBy`                                                                                      | [components.SearchUsersPayloadGroupBy](../../models/components/searchuserspayloadgroupby.md)   | :heavy_minus_sign:                                                                             | Grouping dimension for results                                                                 |
| `limit`                                                                                        | *number*                                                                                       | :heavy_minus_sign:                                                                             | Number of items to return (1-1000)                                                             |
| `sort`                                                                                         | [components.SearchUsersPayloadSort](../../models/components/searchuserspayloadsort.md)         | :heavy_minus_sign:                                                                             | Sort order                                                                                     |
| `userType`                                                                                     | [components.SearchUsersPayloadUserType](../../models/components/searchuserspayloadusertype.md) | :heavy_check_mark:                                                                             | Type of user identifier to group by                                                            |