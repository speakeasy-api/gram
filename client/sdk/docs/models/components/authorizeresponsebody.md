# AuthorizeResponseBody

## Example Usage

```typescript
import { AuthorizeResponseBody } from "@gram/client/models/components/authorizeresponsebody.js";

let value: AuthorizeResponseBody = {
  code: "<value>",
  expiresIn: 543444,
};
```

## Fields

| Field                                                                                             | Type                                                                                              | Required                                                                                          | Description                                                                                       |
| ------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------- |
| `code`                                                                                            | *string*                                                                                          | :heavy_check_mark:                                                                                | The opaque one-time code. Hand this to the device agent, which redeems it with its code_verifier. |
| `expiresIn`                                                                                       | *number*                                                                                          | :heavy_check_mark:                                                                                | Lifetime of the code in seconds.                                                                  |