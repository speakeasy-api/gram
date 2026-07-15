# UpdateRiskExclusionRequest

## Example Usage

```typescript
import { UpdateRiskExclusionRequest } from "@gram/client/models/operations/updateriskexclusion.js";

let value: UpdateRiskExclusionRequest = {
  updateRiskExclusionRequestBody: {
    id: "2b677ad8-ecc1-409f-8763-6d255564f547",
    matchType: "entity_type",
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
| `updateRiskExclusionRequestBody` | [components.UpdateRiskExclusionRequestBody](../../models/components/updateriskexclusionrequestbody.md) | :heavy_check_mark: | N/A            |
