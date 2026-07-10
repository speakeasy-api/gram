# ListRiskExclusionsRequest

## Example Usage

```typescript
import { ListRiskExclusionsRequest } from "@gram/client/models/operations/listriskexclusions.js";

let value: ListRiskExclusionsRequest = {};
```

## Fields

| Field                                                                                                | Type                                                                                                 | Required                                                                                             | Description                                                                                          |
| ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| `riskPolicyId`                                                                                       | *string*                                                                                             | :heavy_minus_sign:                                                                                   | Filter to exclusions bound to this policy. Omit to return all exclusions (global plus every policy). |
| `gramKey`                                                                                            | *string*                                                                                             | :heavy_minus_sign:                                                                                   | API Key header                                                                                       |
| `gramSession`                                                                                        | *string*                                                                                             | :heavy_minus_sign:                                                                                   | Session header                                                                                       |
| `gramProject`                                                                                        | *string*                                                                                             | :heavy_minus_sign:                                                                                   | project header                                                                                       |