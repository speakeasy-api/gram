# EvaluatePromptGuardrailRequest

## Example Usage

```typescript
import { EvaluatePromptGuardrailRequest } from "@gram/client/models/operations/evaluatepromptguardrail.js";

let value: EvaluatePromptGuardrailRequest = {
  evaluatePromptGuardrailRequestBody: {
    chatId: "e64b9e42-1f6c-4861-9dc6-d9292d8741ea",
    prompt: "<value>",
  },
};
```

## Fields

| Field                                                                                                          | Type                                                                                                           | Required                                                                                                       | Description                                                                                                    |
| -------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| `gramKey`                                                                                                      | *string*                                                                                                       | :heavy_minus_sign:                                                                                             | API Key header                                                                                                 |
| `gramSession`                                                                                                  | *string*                                                                                                       | :heavy_minus_sign:                                                                                             | Session header                                                                                                 |
| `gramProject`                                                                                                  | *string*                                                                                                       | :heavy_minus_sign:                                                                                             | project header                                                                                                 |
| `evaluatePromptGuardrailRequestBody`                                                                           | [components.EvaluatePromptGuardrailRequestBody](../../models/components/evaluatepromptguardrailrequestbody.md) | :heavy_check_mark:                                                                                             | N/A                                                                                                            |