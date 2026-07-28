# ChallengeBucket

A group of consecutive challenges with the same dimensions that occurred within a 10-minute window.

## Example Usage

```typescript
import { ChallengeBucket } from "@gram/client/models/components/challengebucket.js";

let value: ChallengeBucket = {
  challengeCount: 226164,
  challengeIds: ["<value 1>", "<value 2>"],
  evaluatedGrantCount: 906850,
  firstSeen: new Date("2024-08-12T17:57:59.195Z"),
  id: "<id>",
  lastSeen: new Date("2025-03-04T19:07:36.790Z"),
  matchedGrantCount: 941143,
  operation: "require",
  organizationId: "<id>",
  outcome: "allow",
  principalType: "api_key",
  principalUrn: "<value>",
  reason: "no_grants",
  roleSlugs: ["<value 1>"],
  scope: "<value>",
};
```

## Fields

| Field                 | Type                                                                                          | Required           | Description                                              |
| --------------------- | --------------------------------------------------------------------------------------------- | ------------------ | -------------------------------------------------------- |
| `challengeCount`      | _number_                                                                                      | :heavy_check_mark: | Number of individual challenges in this bucket.          |
| `challengeIds`        | _string_[]                                                                                    | :heavy_check_mark: | IDs of all challenges in this bucket.                    |
| `evaluatedGrantCount` | _number_                                                                                      | :heavy_check_mark: | Total grants evaluated.                                  |
| `firstSeen`           | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | Timestamp of the earliest challenge in the bucket.       |
| `id`                  | _string_                                                                                      | :heavy_check_mark: | ID of the most recent challenge in the bucket.           |
| `lastSeen`            | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | Timestamp of the most recent challenge in the bucket.    |
| `matchedGrantCount`   | _number_                                                                                      | :heavy_check_mark: | Number of grants that matched.                           |
| `operation`           | [components.Operation](../../models/components/operation.md)                                  | :heavy_check_mark: | N/A                                                      |
| `organizationId`      | _string_                                                                                      | :heavy_check_mark: | Organization the principal was acting in.                |
| `outcome`             | [components.Outcome](../../models/components/outcome.md)                                      | :heavy_check_mark: | N/A                                                      |
| `photoUrl`            | _string_                                                                                      | :heavy_minus_sign: | User avatar URL when available.                          |
| `principalType`       | [components.PrincipalType](../../models/components/principaltype.md)                          | :heavy_check_mark: | Kind of principal.                                       |
| `principalUrn`        | _string_                                                                                      | :heavy_check_mark: | Principal URN e.g. user:<uuid> or api_key:<id>.          |
| `projectId`           | _string_                                                                                      | :heavy_minus_sign: | Project scope (empty for org-level checks).              |
| `reason`              | [components.Reason](../../models/components/reason.md)                                        | :heavy_check_mark: | N/A                                                      |
| `resolutionRoleSlug`  | _string_                                                                                      | :heavy_minus_sign: | Role slug assigned (when resolution_type=role_assigned). |
| `resolutionType`      | [components.ResolutionType](../../models/components/resolutiontype.md)                        | :heavy_minus_sign: | How the bucket was resolved.                             |
| `resolvedAt`          | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign: | When the bucket was resolved by an admin.                |
| `resolvedBy`          | _string_                                                                                      | :heavy_minus_sign: | URN of the admin who resolved.                           |
| `resourceId`          | _string_                                                                                      | :heavy_minus_sign: | Resource ID of the check.                                |
| `resourceKind`        | _string_                                                                                      | :heavy_minus_sign: | Resource kind of the check.                              |
| `roleSlugs`           | _string_[]                                                                                    | :heavy_check_mark: | Roles the principal had loaded.                          |
| `scope`               | _string_                                                                                      | :heavy_check_mark: | Scope that was checked.                                  |
| `userEmail`           | _string_                                                                                      | :heavy_minus_sign: | Email when available.                                    |
