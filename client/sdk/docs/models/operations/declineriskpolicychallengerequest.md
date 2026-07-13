# DeclineRiskPolicyChallengeRequest

## Example Usage

```typescript
import { DeclineRiskPolicyChallengeRequest } from "@gram/client/models/operations/declineriskpolicychallenge.js";

let value: DeclineRiskPolicyChallengeRequest = {
  acknowledgeRiskPolicyChallengeRequestBody: {
    ackToken: "<value>",
  },
};
```

## Fields

| Field                                                                                                                        | Type                                                                                                                         | Required                                                                                                                     | Description                                                                                                                  |
| ---------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| `gramSession`                                                                                                                | *string*                                                                                                                     | :heavy_minus_sign:                                                                                                           | Session header                                                                                                               |
| `acknowledgeRiskPolicyChallengeRequestBody`                                                                                  | [components.AcknowledgeRiskPolicyChallengeRequestBody](../../models/components/acknowledgeriskpolicychallengerequestbody.md) | :heavy_check_mark:                                                                                                           | N/A                                                                                                                          |