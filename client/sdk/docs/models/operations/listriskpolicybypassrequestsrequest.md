# ListRiskPolicyBypassRequestsRequest

## Example Usage

```typescript
import { ListRiskPolicyBypassRequestsRequest } from "@gram/client/models/operations/listriskpolicybypassrequests.js";

let value: ListRiskPolicyBypassRequestsRequest = {};
```

## Fields

| Field         | Type                                                                       | Required           | Description                     |
| ------------- | -------------------------------------------------------------------------- | ------------------ | ------------------------------- |
| `policyId`    | _string_                                                                   | :heavy_minus_sign: | Optional risk policy ID filter. |
| `status`      | [operations.QueryParamStatus](../../models/operations/queryparamstatus.md) | :heavy_minus_sign: | Optional request status filter. |
| `gramKey`     | _string_                                                                   | :heavy_minus_sign: | API Key header                  |
| `gramSession` | _string_                                                                   | :heavy_minus_sign: | Session header                  |
| `gramProject` | _string_                                                                   | :heavy_minus_sign: | project header                  |
