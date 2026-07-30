# RemovePrincipalGrantsRequestBody

## Example Usage

```typescript
import { RemovePrincipalGrantsRequestBody } from "@gram/client/models/components";

let value: RemovePrincipalGrantsRequestBody = {
  principalUrn: "<value>",
};
```

## Fields

| Field          | Type     | Required           | Description                                                                           |
| -------------- | -------- | ------------------ | ------------------------------------------------------------------------------------- |
| `principalUrn` | _string_ | :heavy_check_mark: | The user or role to revoke all permissions from (e.g. "user:user_abc", "role:admin"). |
