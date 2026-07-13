# AcknowledgeRiskPolicyChallengeResponseBody

## Example Usage

```typescript
import { AcknowledgeRiskPolicyChallengeResponseBody } from "@gram/client/models/components/acknowledgeriskpolicychallengeresponsebody.js";

let value: AcknowledgeRiskPolicyChallengeResponseBody = {
  acknowledged: true,
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `acknowledged`                                                                                | *boolean*                                                                                     | :heavy_check_mark:                                                                            | Whether the challenge is now acknowledged.                                                    |
| `expiresAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign:                                                                            | RFC3339 time until which the acknowledgement suppresses re-challenge.                         |
| `policyName`                                                                                  | *string*                                                                                      | :heavy_minus_sign:                                                                            | The policy that issued the warning.                                                           |