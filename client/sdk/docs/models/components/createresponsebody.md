# CreateResponseBody

## Example Usage

```typescript
import { CreateResponseBody } from "@gram/client/models/components/createresponsebody.js";

let value: CreateResponseBody = {
  clientToken: "<value>",
  embedOrigin: "<value>",
  expiresAfter: 740942,
  status: "<value>",
};
```

## Fields

| Field            | Type     | Required           | Description                                  |
| ---------------- | -------- | ------------------ | -------------------------------------------- |
| `clientToken`    | _string_ | :heavy_check_mark: | JWT token for chat session                   |
| `embedOrigin`    | _string_ | :heavy_check_mark: | The origin from which the token will be used |
| `expiresAfter`   | _number_ | :heavy_check_mark: | Token expiration in seconds                  |
| `status`         | _string_ | :heavy_check_mark: | Session status                               |
| `userIdentifier` | _string_ | :heavy_minus_sign: | User identifier if provided                  |
