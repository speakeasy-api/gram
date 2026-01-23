# CreateRequestBody

## Example Usage

```typescript
import { CreateRequestBody } from "@gram/client/models/components";

let value: CreateRequestBody = {
  embedOrigin: "<value>",
};
```

## Fields

| Field                                            | Type                                             | Required                                         | Description                                      |
| ------------------------------------------------ | ------------------------------------------------ | ------------------------------------------------ | ------------------------------------------------ |
| `embedOrigin`                                    | *string*                                         | :heavy_check_mark:                               | The origin from which the token will be used     |
| `expiresAfter`                                   | *number*                                         | :heavy_minus_sign:                               | Token expiration in seconds (max / default 3600) |
| `userIdentifier`                                 | *string*                                         | :heavy_minus_sign:                               | Optional free-form user identifier               |