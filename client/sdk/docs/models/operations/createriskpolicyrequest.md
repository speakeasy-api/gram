# CreateRiskPolicyRequest

## Example Usage

```typescript
import { CreateRiskPolicyRequest } from "@gram/client/models/operations/createriskpolicy.js";

let value: CreateRiskPolicyRequest = {
  createRiskPolicyRequestBody: {
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
| `createRiskPolicyRequestBody` | [components.CreateRiskPolicyRequestBody](../../models/components/createriskpolicyrequestbody.md) | :heavy_check_mark: | N/A            |
