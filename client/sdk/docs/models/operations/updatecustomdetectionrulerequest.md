# UpdateCustomDetectionRuleRequest

## Example Usage

```typescript
import { UpdateCustomDetectionRuleRequest } from "@gram/client/models/operations/updatecustomdetectionrule.js";

let value: UpdateCustomDetectionRuleRequest = {
  updateCustomDetectionRuleRequestBody: {
    id: "fe3905e9-5085-49bc-97dc-3ba43008ca43",
    severity: "critical",
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
| `updateCustomDetectionRuleRequestBody` | [components.UpdateCustomDetectionRuleRequestBody](../../models/components/updatecustomdetectionrulerequestbody.md) | :heavy_check_mark: | N/A            |
