# RegisterRequest

## Example Usage

```typescript
import { RegisterRequest } from "@gram/client/models/operations";

let value: RegisterRequest = {
  registerRequestBody: {
    orgName: "<value>",
  },
};
```

## Fields

| Field                                                                            | Type                                                                             | Required                                                                         | Description                                                                      |
| -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| `gramSession`                                                                    | *string*                                                                         | :heavy_minus_sign:                                                               | Session header                                                                   |
| `registerRequestBody`                                                            | [components.RegisterRequestBody](../../models/components/registerrequestbody.md) | :heavy_check_mark:                                                               | N/A                                                                              |