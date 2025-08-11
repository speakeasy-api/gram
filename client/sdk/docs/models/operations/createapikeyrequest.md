# CreateAPIKeyRequest

## Example Usage

```typescript
import { CreateAPIKeyRequest } from "@gram/client/models/operations";

let value: CreateAPIKeyRequest = {
  createKeyForm: {
    name: "<value>",
    scopes: [
      "<value 1>",
      "<value 2>",
    ],
  },
};
```

## Fields

| Field                                                                | Type                                                                 | Required                                                             | Description                                                          |
| -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- |
| `gramSession`                                                        | *string*                                                             | :heavy_minus_sign:                                                   | Session header                                                       |
| `createKeyForm`                                                      | [components.CreateKeyForm](../../models/components/createkeyform.md) | :heavy_check_mark:                                                   | N/A                                                                  |