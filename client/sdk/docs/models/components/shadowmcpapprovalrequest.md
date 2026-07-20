# ShadowMCPApprovalRequest

## Example Usage

```typescript
import { ShadowMCPApprovalRequest } from "@gram/client/models/components/shadowmcpapprovalrequest.js";

let value: ShadowMCPApprovalRequest = {
  blockedCount: 742620,
  createdAt: new Date("2025-02-22T17:06:11.964Z"),
  id: "6c707894-1a31-49fb-aa6a-bc3375d28d62",
  organizationId: "<id>",
  projectId: "30569cc7-b0f3-47eb-be66-9a4e440f31ed",
  requestedAt: new Date("2024-12-10T09:16:26.341Z"),
  resourceType: "<value>",
  status: "approved",
  updatedAt: new Date("2024-02-17T15:55:15.731Z"),
};
```

## Fields

| Field                    | Type                                                                                          | Required           | Description |
| ------------------------ | --------------------------------------------------------------------------------------------- | ------------------ | ----------- |
| `blockReason`            | _string_                                                                                      | :heavy_minus_sign: | N/A         |
| `blockedCount`           | _number_                                                                                      | :heavy_check_mark: | N/A         |
| `createdAt`              | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | N/A         |
| `decidedAt`              | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign: | N/A         |
| `decidedBy`              | _string_                                                                                      | :heavy_minus_sign: | N/A         |
| `decisionNote`           | _string_                                                                                      | :heavy_minus_sign: | N/A         |
| `firstBlockedAt`         | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign: | N/A         |
| `id`                     | _string_                                                                                      | :heavy_check_mark: | N/A         |
| `lastBlockedAt`          | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign: | N/A         |
| `observedFullUrl`        | _string_                                                                                      | :heavy_minus_sign: | N/A         |
| `observedName`           | _string_                                                                                      | :heavy_minus_sign: | N/A         |
| `observedServerIdentity` | _string_                                                                                      | :heavy_minus_sign: | N/A         |
| `observedUrlHost`        | _string_                                                                                      | :heavy_minus_sign: | N/A         |
| `organizationId`         | _string_                                                                                      | :heavy_check_mark: | N/A         |
| `projectId`              | _string_                                                                                      | :heavy_check_mark: | N/A         |
| `requestedAt`            | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | N/A         |
| `requesterDisplayName`   | _string_                                                                                      | :heavy_minus_sign: | N/A         |
| `requesterEmail`         | _string_                                                                                      | :heavy_minus_sign: | N/A         |
| `requesterUserId`        | _string_                                                                                      | :heavy_minus_sign: | N/A         |
| `resourceType`           | _string_                                                                                      | :heavy_check_mark: | N/A         |
| `riskPolicyId`           | _string_                                                                                      | :heavy_minus_sign: | N/A         |
| `riskResultId`           | _string_                                                                                      | :heavy_minus_sign: | N/A         |
| `status`                 | [components.Status](../../models/components/status.md)                                        | :heavy_check_mark: | N/A         |
| `toolCall`               | _string_                                                                                      | :heavy_minus_sign: | N/A         |
| `toolName`               | _string_                                                                                      | :heavy_minus_sign: | N/A         |
| `updatedAt`              | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | N/A         |
