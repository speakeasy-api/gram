# HooksUserSummary

Aggregated hooks metrics for a single user

## Example Usage

```typescript
import { HooksUserSummary } from "@gram/client/models/components/hooksusersummary.js";

let value: HooksUserSummary = {
  eventCount: 414827,
  failureCount: 831869,
  failureRate: 7032.26,
  successCount: 214493,
  uniqueTools: 303407,
  userEmail: "<value>",
};
```

## Fields

| Field                                                         | Type                                                          | Required                                                      | Description                                                   |
| ------------------------------------------------------------- | ------------------------------------------------------------- | ------------------------------------------------------------- | ------------------------------------------------------------- |
| `eventCount`                                                  | *number*                                                      | :heavy_check_mark:                                            | Total number of hook events for this user                     |
| `failureCount`                                                | *number*                                                      | :heavy_check_mark:                                            | Number of failed tool completions (PostToolUseFailure events) |
| `failureRate`                                                 | *number*                                                      | :heavy_check_mark:                                            | Failure rate as a decimal (0.0 to 1.0)                        |
| `successCount`                                                | *number*                                                      | :heavy_check_mark:                                            | Number of successful tool completions (PostToolUse events)    |
| `uniqueTools`                                                 | *number*                                                      | :heavy_check_mark:                                            | Number of unique tools used by this user                      |
| `userEmail`                                                   | *string*                                                      | :heavy_check_mark:                                            | User email address                                            |