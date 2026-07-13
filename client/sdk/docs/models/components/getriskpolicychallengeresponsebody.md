# GetRiskPolicyChallengeResponseBody

## Example Usage

```typescript
import { GetRiskPolicyChallengeResponseBody } from "@gram/client/models/components/getriskpolicychallengeresponsebody.js";

let value: GetRiskPolicyChallengeResponseBody = {
  acknowledged: false,
  message: "<value>",
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `acknowledged`                                                                                | *boolean*                                                                                     | :heavy_check_mark:                                                                            | Whether this challenge has already been acknowledged.                                         |
| `expiresAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign:                                                                            | RFC3339 time the acknowledgement link expires.                                                |
| `message`                                                                                     | *string*                                                                                      | :heavy_check_mark:                                                                            | Human-facing challenge message describing what was flagged.                                   |
| `policyName`                                                                                  | *string*                                                                                      | :heavy_minus_sign:                                                                            | The policy that issued the warning.                                                           |
| `toolName`                                                                                    | *string*                                                                                      | :heavy_minus_sign:                                                                            | The tool the challenge applies to, if any.                                                    |