# OnboardingStatusResult

## Example Usage

```typescript
import { OnboardingStatusResult } from "@gram/client/models/components/onboardingstatusresult.js";

let value: OnboardingStatusResult = {
  dsyncConfigured: true,
  ssoConfigured: true,
};
```

## Fields

| Field                                                                      | Type                                                                       | Required                                                                   | Description                                                                |
| -------------------------------------------------------------------------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| `dsyncConfigured`                                                          | *boolean*                                                                  | :heavy_check_mark:                                                         | Whether the organization has at least one linked directory sync in WorkOS. |
| `ssoConfigured`                                                            | *boolean*                                                                  | :heavy_check_mark:                                                         | Whether the organization has at least one active SSO connection in WorkOS. |