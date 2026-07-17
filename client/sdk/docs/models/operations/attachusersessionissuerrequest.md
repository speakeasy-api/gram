# AttachUserSessionIssuerRequest

## Example Usage

```typescript
import { AttachUserSessionIssuerRequest } from "@gram/client/models/operations/attachusersessionissuer.js";

let value: AttachUserSessionIssuerRequest = {
  attachUserSessionIssuerForm: {
    id: "b7afe2ac-97d8-410d-91e6-70360129b939",
    userSessionIssuerId: "7265b04e-41b7-4587-8320-221f8d63f3e2",
  },
};
```

## Fields

| Field                         | Type                                                                                             | Required           | Description    |
| ----------------------------- | ------------------------------------------------------------------------------------------------ | ------------------ | -------------- |
| `gramSession`                 | _string_                                                                                         | :heavy_minus_sign: | Session header |
| `gramKey`                     | _string_                                                                                         | :heavy_minus_sign: | API Key header |
| `gramProject`                 | _string_                                                                                         | :heavy_minus_sign: | project header |
| `attachUserSessionIssuerForm` | [components.AttachUserSessionIssuerForm](../../models/components/attachusersessionissuerform.md) | :heavy_check_mark: | N/A            |
