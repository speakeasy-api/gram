# UpdateMemberRolesRequest

## Example Usage

```typescript
import { UpdateMemberRolesRequest } from "@gram/client/models/operations/updatememberroles.js";

let value: UpdateMemberRolesRequest = {
  updateMemberRolesForm: {
    roleIds: ["<value 1>"],
    userId: "<id>",
  },
};
```

## Fields

| Field                   | Type                                                                                 | Required           | Description    |
| ----------------------- | ------------------------------------------------------------------------------------ | ------------------ | -------------- |
| `gramKey`               | _string_                                                                             | :heavy_minus_sign: | API Key header |
| `gramSession`           | _string_                                                                             | :heavy_minus_sign: | Session header |
| `updateMemberRolesForm` | [components.UpdateMemberRolesForm](../../models/components/updatememberrolesform.md) | :heavy_check_mark: | N/A            |
