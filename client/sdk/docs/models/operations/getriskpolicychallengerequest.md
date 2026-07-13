# GetRiskPolicyChallengeRequest

## Example Usage

```typescript
import { GetRiskPolicyChallengeRequest } from "@gram/client/models/operations/getriskpolicychallenge.js";

let value: GetRiskPolicyChallengeRequest = {
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