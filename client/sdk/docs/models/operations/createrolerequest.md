# CreateRoleRequest

## Example Usage

```typescript
import { CreateRoleRequest } from "@gram/client/models/operations/createrole.js";

let value: CreateRoleRequest = {
  createRoleForm: {
    description: "than before out whoa nutritious",
    grants: [],
    name: "<value>",
  },
};
```

## Fields

| Field            | Type                                                                   | Required           | Description    |
| ---------------- | ---------------------------------------------------------------------- | ------------------ | -------------- |
| `gramKey`        | _string_                                                               | :heavy_minus_sign: | API Key header |
| `gramSession`    | _string_                                                               | :heavy_minus_sign: | Session header |
| `createRoleForm` | [components.CreateRoleForm](../../models/components/createroleform.md) | :heavy_check_mark: | N/A            |
