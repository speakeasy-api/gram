# SearchChatsFilter

Filter criteria for searching chat sessions

## Example Usage

```typescript
import { SearchChatsFilter } from "@gram/client/models/components";

let value: SearchChatsFilter = {
  from: new Date("2025-12-19T10:00:00Z"),
  to: new Date("2025-12-19T11:00:00Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   | Example                                                                                       |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `deploymentId`                                                                                | *string*                                                                                      | :heavy_minus_sign:                                                                            | Deployment ID filter                                                                          |                                                                                               |
| `from`                                                                                        | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign:                                                                            | Start time in ISO 8601 format (e.g., '2025-12-19T10:00:00Z')                                  | 2025-12-19T10:00:00Z                                                                          |
| `gramUrn`                                                                                     | *string*                                                                                      | :heavy_minus_sign:                                                                            | Gram URN filter (single URN, use gram_urns for multiple)                                      |                                                                                               |
| `to`                                                                                          | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign:                                                                            | End time in ISO 8601 format (e.g., '2025-12-19T11:00:00Z')                                    | 2025-12-19T11:00:00Z                                                                          |