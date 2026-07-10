# TestDetectionRuleRequest

## Example Usage

```typescript
import { TestDetectionRuleRequest } from "@gram/client/models/operations/testdetectionrule.js";

let value: TestDetectionRuleRequest = {
  testDetectionRuleRequestBody: {
    ruleId: "<id>",
    text: "<value>",
  },
};
```

## Fields

| Field                                                                                              | Type                                                                                               | Required                                                                                           | Description                                                                                        |
| -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| `gramKey`                                                                                          | *string*                                                                                           | :heavy_minus_sign:                                                                                 | API Key header                                                                                     |
| `gramSession`                                                                                      | *string*                                                                                           | :heavy_minus_sign:                                                                                 | Session header                                                                                     |
| `gramProject`                                                                                      | *string*                                                                                           | :heavy_minus_sign:                                                                                 | project header                                                                                     |
| `testDetectionRuleRequestBody`                                                                     | [components.TestDetectionRuleRequestBody](../../models/components/testdetectionrulerequestbody.md) | :heavy_check_mark:                                                                                 | N/A                                                                                                |