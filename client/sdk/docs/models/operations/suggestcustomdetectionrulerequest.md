# SuggestCustomDetectionRuleRequest

## Example Usage

```typescript
import { SuggestCustomDetectionRuleRequest } from "@gram/client/models/operations/suggestcustomdetectionrule.js";

let value: SuggestCustomDetectionRuleRequest = {
  suggestCustomDetectionRuleRequestBody: {
    prompt: "<value>",
  },
};
```

## Fields

| Field                                                                                                                | Type                                                                                                                 | Required                                                                                                             | Description                                                                                                          |
| -------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- |
| `gramKey`                                                                                                            | *string*                                                                                                             | :heavy_minus_sign:                                                                                                   | API Key header                                                                                                       |
| `gramSession`                                                                                                        | *string*                                                                                                             | :heavy_minus_sign:                                                                                                   | Session header                                                                                                       |
| `gramProject`                                                                                                        | *string*                                                                                                             | :heavy_minus_sign:                                                                                                   | project header                                                                                                       |
| `suggestCustomDetectionRuleRequestBody`                                                                              | [components.SuggestCustomDetectionRuleRequestBody](../../models/components/suggestcustomdetectionrulerequestbody.md) | :heavy_check_mark:                                                                                                   | N/A                                                                                                                  |