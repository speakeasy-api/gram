# UpdateRiskPolicyRequest

## Example Usage

```typescript
import { UpdateRiskPolicyRequest } from "@gram/client/models/operations/updateriskpolicy.js";

let value: UpdateRiskPolicyRequest = {
  updateRiskPolicyRequestBody: {
    id: "09ccee4e-2150-46ea-85a8-829b533f156e",
    name: "<value>",
    presidioScoreThreshold: 0.75,
  },
};
```

## Fields

| Field                         | Type                                                                                             | Required           | Description    |
| ----------------------------- | ------------------------------------------------------------------------------------------------ | ------------------ | -------------- |
| `gramKey`                     | _string_                                                                                         | :heavy_minus_sign: | API Key header |
| `gramSession`                 | _string_                                                                                         | :heavy_minus_sign: | Session header |
| `gramProject`                 | _string_                                                                                         | :heavy_minus_sign: | project header |
| `updateRiskPolicyRequestBody` | [components.UpdateRiskPolicyRequestBody](../../models/components/updateriskpolicyrequestbody.md) | :heavy_check_mark: | N/A            |
