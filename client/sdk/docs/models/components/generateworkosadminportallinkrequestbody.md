# GenerateWorkOSAdminPortalLinkRequestBody

## Example Usage

```typescript
import { GenerateWorkOSAdminPortalLinkRequestBody } from "@gram/client/models/components/generateworkosadminportallinkrequestbody.js";

let value: GenerateWorkOSAdminPortalLinkRequestBody = {
  intent: "audit_logs",
};
```

## Fields

| Field             | Type                                                                             | Required           | Description                                                                    |
| ----------------- | -------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------ |
| `intent`          | [components.Intent](../../models/components/intent.md)                           | :heavy_check_mark: | WorkOS Admin Portal intent.                                                    |
| `intentOptions`   | [components.WorkOSIntentOptions](../../models/components/workosintentoptions.md) | :heavy_minus_sign: | N/A                                                                            |
| `itContactEmails` | _string_[]                                                                       | :heavy_minus_sign: | IT contact email addresses displayed in the Admin Portal for end-user support. |
| `returnUrl`       | _string_                                                                         | :heavy_minus_sign: | URL to redirect the user to after the Admin Portal session ends.               |
| `successUrl`      | _string_                                                                         | :heavy_minus_sign: | URL to redirect the user to on successful completion of the Admin Portal flow. |
