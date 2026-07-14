# CreateCustomDetectionRuleRequest

## Example Usage

```typescript
import { CreateCustomDetectionRuleRequest } from "@gram/client/models/operations/createcustomdetectionrule.js";

let value: CreateCustomDetectionRuleRequest = {
  createCustomDetectionRuleRequestBody: {
    ruleId: "<id>",
    title: "<value>",
  },
};
```

## Fields

| Field                                  | Type                                                                                                               | Required           | Description    |
| -------------------------------------- | ------------------------------------------------------------------------------------------------------------------ | ------------------ | -------------- |
| `gramKey`                              | _string_                                                                                                           | :heavy_minus_sign: | API Key header |
| `gramSession`                          | _string_                                                                                                           | :heavy_minus_sign: | Session header |
| `gramProject`                          | _string_                                                                                                           | :heavy_minus_sign: | project header |
| `createCustomDetectionRuleRequestBody` | [components.CreateCustomDetectionRuleRequestBody](../../models/components/createcustomdetectionrulerequestbody.md) | :heavy_check_mark: | N/A            |
