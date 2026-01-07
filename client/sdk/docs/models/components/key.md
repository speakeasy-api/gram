# Key

## Example Usage

```typescript
import { Key } from "@gram/client/models/components";

let value: Key = {
  createdAt: new Date("2026-10-29T20:25:41.722Z"),
  createdByUserId: "<id>",
  id: "<id>",
  keyPrefix: "<value>",
  name: "<value>",
  organizationId: "<id>",
  scopes: [
    "<value 1>",
    "<value 2>",
    "<value 3>",
  ],
  updatedAt: new Date("2025-01-15T10:52:26.209Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | The creation date of the key.                                                                 |
| `createdByUserId`                                                                             | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the user who created this key                                                       |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the key                                                                             |
| `key`                                                                                         | *string*                                                                                      | :heavy_minus_sign:                                                                            | The token of the api key (only returned on key creation)                                      |
| `keyPrefix`                                                                                   | *string*                                                                                      | :heavy_check_mark:                                                                            | The store prefix of the api key for recognition                                               |
| `name`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | The name of the key                                                                           |
| `organizationId`                                                                              | *string*                                                                                      | :heavy_check_mark:                                                                            | The organization ID this key belongs to                                                       |
| `projectId`                                                                                   | *string*                                                                                      | :heavy_minus_sign:                                                                            | The optional project ID this key is scoped to                                                 |
| `scopes`                                                                                      | *string*[]                                                                                    | :heavy_check_mark:                                                                            | List of permission scopes for this key                                                        |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the key was last updated.                                                                |