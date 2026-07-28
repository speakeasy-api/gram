# ListRiskExclusionsRequest

## Example Usage

```typescript
import { ListRiskExclusionsRequest } from "@gram/client/models/operations/listriskexclusions.js";

let value: ListRiskExclusionsRequest = {};
```

## Fields

| Field          | Type     | Required           | Description                                                                                          |
| -------------- | -------- | ------------------ | ---------------------------------------------------------------------------------------------------- |
| `riskPolicyId` | _string_ | :heavy_minus_sign: | Filter to exclusions bound to this policy. Omit to return all exclusions (global plus every policy). |
| `gramKey`      | _string_ | :heavy_minus_sign: | API Key header                                                                                       |
| `gramSession`  | _string_ | :heavy_minus_sign: | Session header                                                                                       |
| `gramProject`  | _string_ | :heavy_minus_sign: | project header                                                                                       |
