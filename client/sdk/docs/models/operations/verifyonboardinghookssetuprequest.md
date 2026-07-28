# VerifyOnboardingHooksSetupRequest

## Example Usage

```typescript
import { VerifyOnboardingHooksSetupRequest } from "@gram/client/models/operations/verifyonboardinghookssetup.js";

let value: VerifyOnboardingHooksSetupRequest = {};
```

## Fields

| Field           | Type     | Required           | Description                                                                                                                                                                    |
| --------------- | -------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `sinceUnixNano` | _string_ | :heavy_minus_sign: | Only return events with time_unix_nano greater than this value. Pass the previous response's latest_unix_nano to poll for new events. Stringified to preserve int64 precision. |
| `gramSession`   | _string_ | :heavy_minus_sign: | Session header                                                                                                                                                                 |
