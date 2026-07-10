# Role

## Example Usage

```typescript
import { Role } from "@gram/client/models/components/role.js";

let value: Role = {
  createdAt: new Date("2024-12-05T18:24:49.943Z"),
  description: "lovingly fragrant shabby inexperienced aha",
  grants: [],
  id: "<id>",
  isSystem: false,
  memberCount: 680463,
  name: "<value>",
  principalUrn: "<value>",
  slug: "<value>",
  updatedAt: new Date("2024-07-13T23:09:31.054Z"),
};
```

## Fields

| Field          | Type                                                                                          | Required           | Description                                                    |
| -------------- | --------------------------------------------------------------------------------------------- | ------------------ | -------------------------------------------------------------- |
| `createdAt`    | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | N/A                                                            |
| `description`  | _string_                                                                                      | :heavy_check_mark: | Human-readable description.                                    |
| `grants`       | [components.RoleGrant](../../models/components/rolegrant.md)[]                                | :heavy_check_mark: | Scope grants assigned to this role.                            |
| `id`           | _string_                                                                                      | :heavy_check_mark: | Unique role identifier.                                        |
| `isSystem`     | _boolean_                                                                                     | :heavy_check_mark: | Whether this is a built-in system role that cannot be deleted. |
| `memberCount`  | _number_                                                                                      | :heavy_check_mark: | Number of members assigned to this role.                       |
| `name`         | _string_                                                                                      | :heavy_check_mark: | Display name of the role.                                      |
| `principalUrn` | _string_                                                                                      | :heavy_check_mark: | Canonical principal URN for this role.                         |
| `slug`         | _string_                                                                                      | :heavy_check_mark: | Stable WorkOS role slug.                                       |
| `updatedAt`    | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | N/A                                                            |
