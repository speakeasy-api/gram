# Grant

A permission record giving a user or role the ability to perform an action on a resource.

## Example Usage

```typescript
import { Grant } from "@gram/client/models/components";

let value: Grant = {
  createdAt: new Date("2026-06-17T10:19:59.907Z"),
  id: "0601ad09-7604-4269-8aab-e7f9cec60e93",
  organizationId: "<id>",
  principalType: "<value>",
  principalUrn: "<value>",
  resource: "<value>",
  scope: "<value>",
  updatedAt: new Date("2025-12-22T07:48:47.373Z"),
};
```

## Fields

| Field            | Type                                                                                          | Required           | Description                                                                       |
| ---------------- | --------------------------------------------------------------------------------------------- | ------------------ | --------------------------------------------------------------------------------- |
| `createdAt`      | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | When this permission was granted.                                                 |
| `id`             | _string_                                                                                      | :heavy_check_mark: | Unique identifier of this permission.                                             |
| `organizationId` | _string_                                                                                      | :heavy_check_mark: | The organization this permission belongs to.                                      |
| `principalType`  | _string_                                                                                      | :heavy_check_mark: | Whether the principal is a user or a role.                                        |
| `principalUrn`   | _string_                                                                                      | :heavy_check_mark: | The user or role that holds this permission (e.g. "user:user_abc", "role:admin"). |
| `resource`       | _string_                                                                                      | :heavy_check_mark: | The resource this permission applies to. "\*" means all resources.                |
| `scope`          | _string_                                                                                      | :heavy_check_mark: | The action this permission allows (e.g. "build:read", "mcp:connect").             |
| `updatedAt`      | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | When this permission was last updated.                                            |
