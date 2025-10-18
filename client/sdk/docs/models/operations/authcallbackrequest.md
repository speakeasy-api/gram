# AuthCallbackRequest

## Example Usage

```typescript
import { AuthCallbackRequest } from "@gram/client/models/operations";

let value: AuthCallbackRequest = {
  code: "<value>",
};
```

## Fields

| Field                                                              | Type                                                               | Required                                                           | Description                                                        |
| ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ |
| `code`                                                             | *string*                                                           | :heavy_check_mark:                                                 | The auth code for authentication from the speakeasy system         |
| `state`                                                            | *string*                                                           | :heavy_minus_sign:                                                 | The opaque state string optionally provided during initialization. |