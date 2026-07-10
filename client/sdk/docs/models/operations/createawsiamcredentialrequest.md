# CreateAwsIamCredentialRequest

## Example Usage

```typescript
import { CreateAwsIamCredentialRequest } from "@gram/client/models/operations/createawsiamcredential.js";

let value: CreateAwsIamCredentialRequest = {
  createAwsIamCredentialForm: {
    name: "<value>",
  },
};
```

## Fields

| Field                        | Type                                                                                           | Required           | Description    |
| ---------------------------- | ---------------------------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramSession`                | _string_                                                                                       | :heavy_minus_sign: | Session header |
| `createAwsIamCredentialForm` | [components.CreateAwsIamCredentialForm](../../models/components/createawsiamcredentialform.md) | :heavy_check_mark: | N/A            |
