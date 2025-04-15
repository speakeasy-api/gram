# Key

## Example Usage

```typescript
import { Key } from "@gram/client/models/components";

let value: Key = {
  createdAt: new Date("2025-10-29T20:25:41.722Z"),
  createdByUserId: "<id>",
  id: "<id>",
  name: "<value>",
  organizationId: "<id>",
  scopes: [
    "<value>",
  ],
  token: "<value>",
  updatedAt: new Date("2025-11-12T04:40:01.714Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | The creation date of the key.                                                                 |
| `createdByUserId`                                                                             | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the user who created this key                                                       |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the key                                                                             |
| `name`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | The name of the key                                                                           |
| `organizationId`                                                                              | *string*                                                                                      | :heavy_check_mark:                                                                            | The organization ID this key belongs to                                                       |
| `projectId`                                                                                   | *string*                                                                                      | :heavy_minus_sign:                                                                            | The optional project ID this key is scoped to                                                 |
| `scopes`                                                                                      | *string*[]                                                                                    | :heavy_check_mark:                                                                            | List of permission scopes for this key                                                        |
| `token`                                                                                       | *string*                                                                                      | :heavy_check_mark:                                                                            | The API token value                                                                           |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the key was last updated.                                                                |