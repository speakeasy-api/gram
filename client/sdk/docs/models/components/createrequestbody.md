# CreateRequestBody

## Example Usage

```typescript
import { CreateRequestBody } from "@gram/client/models/components";

let value: CreateRequestBody = {};
```

## Fields

| Field                                               | Type                                                | Required                                            | Description                                         |
| --------------------------------------------------- | --------------------------------------------------- | --------------------------------------------------- | --------------------------------------------------- |
| `expiresAfter`                                      | *number*                                            | :heavy_minus_sign:                                  | Token expiration in seconds (max 3600, default 600) |
| `userIdentifier`                                    | *string*                                            | :heavy_minus_sign:                                  | Optional free-form user identifier                  |