# RevokeAPIKeyRequest

## Example Usage

```typescript
import { RevokeAPIKeyRequest } from "@gram/client/models/operations/revokeapikey.js";

let value: RevokeAPIKeyRequest = {
  id: "<id>",
};
```

## Fields

| Field         | Type     | Required           | Description                 |
| ------------- | -------- | ------------------ | --------------------------- |
| `id`          | _string_ | :heavy_check_mark: | The ID of the key to revoke |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header              |
