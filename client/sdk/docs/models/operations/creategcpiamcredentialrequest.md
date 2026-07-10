# CreateGcpIamCredentialRequest

## Example Usage

```typescript
import { CreateGcpIamCredentialRequest } from "@gram/client/models/operations/creategcpiamcredential.js";

let value: CreateGcpIamCredentialRequest = {
  createGcpIamCredentialForm: {
    name: "<value>",
  },
};
```

## Fields

| Field                        | Type                                                                                           | Required           | Description    |
| ---------------------------- | ---------------------------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramSession`                | _string_                                                                                       | :heavy_minus_sign: | Session header |
| `createGcpIamCredentialForm` | [components.CreateGcpIamCredentialForm](../../models/components/creategcpiamcredentialform.md) | :heavy_check_mark: | N/A            |
