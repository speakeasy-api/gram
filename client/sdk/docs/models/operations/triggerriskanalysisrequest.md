# TriggerRiskAnalysisRequest

## Example Usage

```typescript
import { TriggerRiskAnalysisRequest } from "@gram/client/models/operations/triggerriskanalysis.js";

let value: TriggerRiskAnalysisRequest = {
  triggerRiskAnalysisRequestBody: {
    id: "87306600-d20c-40e4-ada0-8534e07d8435",
  },
};
```

## Fields

| Field                            | Type                                                                                                   | Required           | Description    |
| -------------------------------- | ------------------------------------------------------------------------------------------------------ | ------------------ | -------------- |
| `gramKey`                        | _string_                                                                                               | :heavy_minus_sign: | API Key header |
| `gramSession`                    | _string_                                                                                               | :heavy_minus_sign: | Session header |
| `gramProject`                    | _string_                                                                                               | :heavy_minus_sign: | project header |
| `triggerRiskAnalysisRequestBody` | [components.TriggerRiskAnalysisRequestBody](../../models/components/triggerriskanalysisrequestbody.md) | :heavy_check_mark: | N/A            |
