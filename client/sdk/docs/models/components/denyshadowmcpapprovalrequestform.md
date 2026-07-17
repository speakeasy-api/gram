# DenyShadowMCPApprovalRequestForm

## Example Usage

```typescript
import { DenyShadowMCPApprovalRequestForm } from "@gram/client/models/components/denyshadowmcpapprovalrequestform.js";

let value: DenyShadowMCPApprovalRequestForm = {
  createDenyRule: true,
  id: "a1d7c929-7daa-48d6-aaf8-9614dcf3fdc7",
};
```

## Fields

| Field                    | Type                                                                                                                               | Required           | Description                                                                                   |
| ------------------------ | ---------------------------------------------------------------------------------------------------------------------------------- | ------------------ | --------------------------------------------------------------------------------------------- |
| `createDenyRule`         | _boolean_                                                                                                                          | :heavy_check_mark: | N/A                                                                                           |
| `displayName`            | _string_                                                                                                                           | :heavy_minus_sign: | N/A                                                                                           |
| `id`                     | _string_                                                                                                                           | :heavy_check_mark: | N/A                                                                                           |
| `matchBreadth`           | [components.DenyShadowMCPApprovalRequestFormMatchBreadth](../../models/components/denyshadowmcpapprovalrequestformmatchbreadth.md) | :heavy_minus_sign: | N/A                                                                                           |
| `matchValue`             | _string_                                                                                                                           | :heavy_minus_sign: | N/A                                                                                           |
| `observedFullUrl`        | _string_                                                                                                                           | :heavy_minus_sign: | N/A                                                                                           |
| `observedServerIdentity` | _string_                                                                                                                           | :heavy_minus_sign: | N/A                                                                                           |
| `observedUrlHost`        | _string_                                                                                                                           | :heavy_minus_sign: | N/A                                                                                           |
| `projectIds`             | _string_[]                                                                                                                         | :heavy_minus_sign: | Project ids to create project-scoped deny rules for. Empty falls back to the request project. |
| `reason`                 | _string_                                                                                                                           | :heavy_minus_sign: | N/A                                                                                           |
