# KeysNumberCreateKeyRequest

## Example Usage

```typescript
import { KeysNumberCreateKeyRequest } from "@gram/sdk/models/operations";

let value: KeysNumberCreateKeyRequest = {
  gramSession: "Omnis iure eaque qui qui excepturi.",
  createKeyForm: {
    name: "Sunt debitis harum et nihil rerum reprehenderit.",
  },
};
```

## Fields

| Field                                                                | Type                                                                 | Required                                                             | Description                                                          | Example                                                              |
| -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- |
| `gramSession`                                                        | *string*                                                             | :heavy_minus_sign:                                                   | Session header                                                       | Omnis iure eaque qui qui excepturi.                                  |
| `createKeyForm`                                                      | [components.CreateKeyForm](../../models/components/createkeyform.md) | :heavy_check_mark:                                                   | N/A                                                                  | {<br/>"name": "Sunt debitis harum et nihil rerum reprehenderit."<br/>} |