# GrantEntry

A permission entry identifying who it applies to, what action it covers, and which resource it targets.

## Example Usage

```typescript
import { GrantEntry } from "@gram/client/models/components";

let value: GrantEntry = {
  principalUrn: "<value>",
  resource: "<value>",
  scope: "<value>",
};
```

## Fields

| Field          | Type     | Required           | Description                                                                             |
| -------------- | -------- | ------------------ | --------------------------------------------------------------------------------------- |
| `principalUrn` | _string_ | :heavy_check_mark: | The user or role this permission entry applies to (e.g. "user:user_abc", "role:admin"). |
| `resource`     | _string_ | :heavy_check_mark: | The resource this permission applies to. Use "\*" for unrestricted access.              |
| `scope`        | _string_ | :heavy_check_mark: | The action being permitted (e.g. "build:read", "mcp:connect").                          |
