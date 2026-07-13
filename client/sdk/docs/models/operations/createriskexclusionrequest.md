# CreateRiskExclusionRequest

## Example Usage

```typescript
import { CreateRiskExclusionRequest } from "@gram/client/models/operations/createriskexclusion.js";

let value: CreateRiskExclusionRequest = {
  createRiskExclusionRequestBody: {
    matchType: "exact",
    matchValue: "<value>",
  },
};
```

## Fields

| Field                            | Type                                                                                                   | Required           | Description    |
| -------------------------------- | ------------------------------------------------------------------------------------------------------ | ------------------ | -------------- |
| `gramKey`                        | _string_                                                                                               | :heavy_minus_sign: | API Key header |
| `gramSession`                    | _string_                                                                                               | :heavy_minus_sign: | Session header |
| `gramProject`                    | _string_                                                                                               | :heavy_minus_sign: | project header |
| `createRiskExclusionRequestBody` | [components.CreateRiskExclusionRequestBody](../../models/components/createriskexclusionrequestbody.md) | :heavy_check_mark: | N/A            |
