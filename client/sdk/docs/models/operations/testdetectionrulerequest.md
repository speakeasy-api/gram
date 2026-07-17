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

| Field                          | Type                                                                                               | Required           | Description    |
| ------------------------------ | -------------------------------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramKey`                      | _string_                                                                                           | :heavy_minus_sign: | API Key header |
| `gramSession`                  | _string_                                                                                           | :heavy_minus_sign: | Session header |
| `gramProject`                  | _string_                                                                                           | :heavy_minus_sign: | project header |
| `testDetectionRuleRequestBody` | [components.TestDetectionRuleRequestBody](../../models/components/testdetectionrulerequestbody.md) | :heavy_check_mark: | N/A            |
