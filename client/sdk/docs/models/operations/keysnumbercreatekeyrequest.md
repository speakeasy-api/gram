# KeysNumberCreateKeyRequest

## Example Usage

```typescript
import { KeysNumberCreateKeyRequest } from "@gram/client/models/operations";

let value: KeysNumberCreateKeyRequest = {
  createKeyForm: {
    name: "<value>",
  },
};
```

## Fields

| Field                                                                | Type                                                                 | Required                                                             | Description                                                          |
| -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- |
| `gramSession`                                                        | *string*                                                             | :heavy_minus_sign:                                                   | Session header                                                       |
| `createKeyForm`                                                      | [components.CreateKeyForm](../../models/components/createkeyform.md) | :heavy_check_mark:                                                   | N/A                                                                  |