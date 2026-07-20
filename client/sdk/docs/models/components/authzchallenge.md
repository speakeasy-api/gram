# AuthzChallenge

## Example Usage

```typescript
import { AuthzChallenge } from "@gram/client/models/components/authzchallenge.js";

let value: AuthzChallenge = {
  evaluatedGrantCount: 881863,
  id: "<id>",
  matchedGrantCount: 531276,
  operation: "require_any",
  organizationId: "<id>",
  outcome: "error",
  principalType: "user",
  principalUrn: "<value>",
  reason: "no_grants",
  roleSlugs: ["<value 1>"],
  scope: "<value>",
  timestamp: new Date("2025-05-19T10:21:58.523Z"),
};
```

## Fields

| Field                 | Type                                                                                               | Required           | Description                                              |
| --------------------- | -------------------------------------------------------------------------------------------------- | ------------------ | -------------------------------------------------------- |
| `evaluatedGrantCount` | _number_                                                                                           | :heavy_check_mark: | Total grants evaluated.                                  |
| `id`                  | _string_                                                                                           | :heavy_check_mark: | Unique challenge identifier.                             |
| `matchedGrantCount`   | _number_                                                                                           | :heavy_check_mark: | Number of grants that matched.                           |
| `operation`           | [components.AuthzChallengeOperation](../../models/components/authzchallengeoperation.md)           | :heavy_check_mark: | N/A                                                      |
| `organizationId`      | _string_                                                                                           | :heavy_check_mark: | Organization the principal was acting in.                |
| `outcome`             | [components.AuthzChallengeOutcome](../../models/components/authzchallengeoutcome.md)               | :heavy_check_mark: | N/A                                                      |
| `photoUrl`            | _string_                                                                                           | :heavy_minus_sign: | User avatar URL when available.                          |
| `principalType`       | [components.AuthzChallengePrincipalType](../../models/components/authzchallengeprincipaltype.md)   | :heavy_check_mark: | Kind of principal.                                       |
| `principalUrn`        | _string_                                                                                           | :heavy_check_mark: | Principal URN e.g. user:<uuid> or api_key:<id>.          |
| `projectId`           | _string_                                                                                           | :heavy_minus_sign: | Project scope (empty for org-level checks).              |
| `reason`              | [components.AuthzChallengeReason](../../models/components/authzchallengereason.md)                 | :heavy_check_mark: | N/A                                                      |
| `resolutionRoleSlug`  | _string_                                                                                           | :heavy_minus_sign: | Role slug assigned (when resolution_type=role_assigned). |
| `resolutionType`      | [components.AuthzChallengeResolutionType](../../models/components/authzchallengeresolutiontype.md) | :heavy_minus_sign: | How the challenge was resolved.                          |
| `resolvedAt`          | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date)      | :heavy_minus_sign: | When the challenge was resolved by an admin.             |
| `resolvedBy`          | _string_                                                                                           | :heavy_minus_sign: | URN of the admin who resolved.                           |
| `resourceId`          | _string_                                                                                           | :heavy_minus_sign: | Resource ID of the check.                                |
| `resourceKind`        | _string_                                                                                           | :heavy_minus_sign: | Resource kind of the check.                              |
| `roleSlugs`           | _string_[]                                                                                         | :heavy_check_mark: | Roles the principal had loaded.                          |
| `scope`               | _string_                                                                                           | :heavy_check_mark: | Scope that was checked.                                  |
| `timestamp`           | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date)      | :heavy_check_mark: | When the authz decision was made.                        |
| `userEmail`           | _string_                                                                                           | :heavy_minus_sign: | Email when available.                                    |
