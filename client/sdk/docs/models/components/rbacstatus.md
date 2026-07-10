# RBACStatus

## Example Usage

```typescript
import { RBACStatus } from "@gram/client/models/components/rbacstatus.js";

let value: RBACStatus = {
  rbacEnabled: false,
};
```

## Fields

| Field                                                                | Type                                                                 | Required                                                             | Description                                                          |
| -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- |
| `rbacEnabled`                                                        | *boolean*                                                            | :heavy_check_mark:                                                   | Whether RBAC enforcement is currently enabled for this organization. |