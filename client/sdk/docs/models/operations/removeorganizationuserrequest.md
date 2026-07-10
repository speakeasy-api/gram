# RemoveOrganizationUserRequest

## Example Usage

```typescript
import { RemoveOrganizationUserRequest } from "@gram/client/models/operations/removeorganizationuser.js";

let value: RemoveOrganizationUserRequest = {
  userId: "<id>",
};
```

## Fields

| Field         | Type     | Required           | Description             |
| ------------- | -------- | ------------------ | ----------------------- |
| `userId`      | _string_ | :heavy_check_mark: | Gram user ID to remove. |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header          |
