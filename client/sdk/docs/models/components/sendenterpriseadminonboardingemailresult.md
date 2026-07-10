# SendEnterpriseAdminOnboardingEmailResult

## Example Usage

```typescript
import { SendEnterpriseAdminOnboardingEmailResult } from "@gram/client/models/components/sendenterpriseadminonboardingemailresult.js";

let value: SendEnterpriseAdminOnboardingEmailResult = {
  sentCount: 916066,
  setupLink: "<value>",
};
```

## Fields

| Field                                             | Type                                              | Required                                          | Description                                       |
| ------------------------------------------------- | ------------------------------------------------- | ------------------------------------------------- | ------------------------------------------------- |
| `sentCount`                                       | *number*                                          | :heavy_check_mark:                                | Number of recipients the email was dispatched to. |
| `setupLink`                                       | *string*                                          | :heavy_check_mark:                                | The setup link embedded in the dispatched email.  |