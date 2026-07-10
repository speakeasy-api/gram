# ListSessionsRequest

## Example Usage

```typescript
import { ListSessionsRequest } from "@gram/client/models/operations/listsessions.js";

let value: ListSessionsRequest = {
  listSessionsPayload: {
    from: new Date("2025-12-19T10:00:00Z"),
    to: new Date("2025-12-26T10:00:00Z"),
  },
};
```

## Fields

| Field                                                                            | Type                                                                             | Required                                                                         | Description                                                                      |
| -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| `gramSession`                                                                    | *string*                                                                         | :heavy_minus_sign:                                                               | Session header                                                                   |
| `listSessionsPayload`                                                            | [components.ListSessionsPayload](../../models/components/listsessionspayload.md) | :heavy_check_mark:                                                               | N/A                                                                              |