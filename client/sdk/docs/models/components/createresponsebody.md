# CreateResponseBody

## Example Usage

```typescript
import { CreateResponseBody } from "@gram/client/models/components";

let value: CreateResponseBody = {
  clientToken: "<value>",
  expiresAfter: 740942,
  status: "<value>",
};
```

## Fields

| Field                       | Type                        | Required                    | Description                 |
| --------------------------- | --------------------------- | --------------------------- | --------------------------- |
| `clientToken`               | *string*                    | :heavy_check_mark:          | JWT token for chat session  |
| `expiresAfter`              | *number*                    | :heavy_check_mark:          | Token expiration in seconds |
| `status`                    | *string*                    | :heavy_check_mark:          | Session status              |
| `userIdentifier`            | *string*                    | :heavy_minus_sign:          | User identifier if provided |