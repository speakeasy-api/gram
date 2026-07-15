# VerifyOnboardingHooksSetupResult

## Example Usage

```typescript
import { VerifyOnboardingHooksSetupResult } from "@gram/client/models/components/verifyonboardinghookssetupresult.js";

let value: VerifyOnboardingHooksSetupResult = {
  events: [
    {
      projectSlug: "<value>",
      source: "<value>",
      timeUnixNano: "<value>",
    },
  ],
  latestUnixNano: "<value>",
  totalCount: 306931,
};
```

## Fields

| Field            | Type                                                                               | Required           | Description                                                                                                    |
| ---------------- | ---------------------------------------------------------------------------------- | ------------------ | -------------------------------------------------------------------------------------------------------------- |
| `events`         | [components.OnboardingHookEvent](../../models/components/onboardinghookevent.md)[] | :heavy_check_mark: | Recent hook events, newest first. Truncated to a server-defined limit.                                         |
| `latestUnixNano` | _string_                                                                           | :heavy_check_mark: | Highest time_unix_nano in this batch. Pass back as since_unix_nano on the next poll.                           |
| `totalCount`     | _number_                                                                           | :heavy_check_mark: | Total events received with time_unix_nano greater than since_unix_nano. May exceed len(events) when truncated. |
