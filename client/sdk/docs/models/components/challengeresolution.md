# ChallengeResolution

## Example Usage

```typescript
import { ChallengeResolution } from "@gram/client/models/components/challengeresolution.js";

let value: ChallengeResolution = {
  challengeId: "<id>",
  createdAt: new Date("2025-08-10T16:52:02.879Z"),
  id: "<id>",
  organizationId: "<id>",
  principalUrn: "<value>",
  resolutionType: "role_assigned",
  resolvedBy: "<value>",
  scope: "<value>",
};
```

## Fields

| Field            | Type                                                                                                         | Required           | Description              |
| ---------------- | ------------------------------------------------------------------------------------------------------------ | ------------------ | ------------------------ |
| `challengeId`    | _string_                                                                                                     | :heavy_check_mark: | ClickHouse challenge ID. |
| `createdAt`      | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date)                | :heavy_check_mark: | N/A                      |
| `id`             | _string_                                                                                                     | :heavy_check_mark: | Resolution record ID.    |
| `organizationId` | _string_                                                                                                     | :heavy_check_mark: | Organization ID.         |
| `principalUrn`   | _string_                                                                                                     | :heavy_check_mark: | Denied principal.        |
| `resolutionType` | [components.ChallengeResolutionResolutionType](../../models/components/challengeresolutionresolutiontype.md) | :heavy_check_mark: | N/A                      |
| `resolvedBy`     | _string_                                                                                                     | :heavy_check_mark: | Admin who resolved.      |
| `resourceId`     | _string_                                                                                                     | :heavy_minus_sign: | Resource ID.             |
| `resourceKind`   | _string_                                                                                                     | :heavy_minus_sign: | Resource kind.           |
| `roleSlug`       | _string_                                                                                                     | :heavy_minus_sign: | Assigned role slug.      |
| `scope`          | _string_                                                                                                     | :heavy_check_mark: | Denied scope.            |
