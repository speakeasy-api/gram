# GrantsForm

A batch of permission entries to apply to access-management operations.

## Example Usage

```typescript
import { GrantsForm } from "@gram/client/models/components";

let value: GrantsForm = {
  grants: [
    {
      principalUrn: "<value>",
      resource: "<value>",
      scope: "<value>",
    },
  ],
};
```

## Fields

| Field    | Type                                                             | Required           | Description                 |
| -------- | ---------------------------------------------------------------- | ------------------ | --------------------------- |
| `grants` | [components.GrantEntry](../../models/components/grantentry.md)[] | :heavy_check_mark: | The permissions to process. |
