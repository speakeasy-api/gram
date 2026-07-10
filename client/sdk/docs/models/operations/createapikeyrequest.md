# CreateAPIKeyRequest

## Example Usage

```typescript
import { CreateAPIKeyRequest } from "@gram/client/models/operations/createapikey.js";

let value: CreateAPIKeyRequest = {
  createKeyForm: {
    name: "<value>",
    scopes: ["<value 1>", "<value 2>"],
  },
};
```

## Fields

| Field           | Type                                                                 | Required           | Description    |
| --------------- | -------------------------------------------------------------------- | ------------------ | -------------- |
| `gramSession`   | _string_                                                             | :heavy_minus_sign: | Session header |
| `createKeyForm` | [components.CreateKeyForm](../../models/components/createkeyform.md) | :heavy_check_mark: | N/A            |
