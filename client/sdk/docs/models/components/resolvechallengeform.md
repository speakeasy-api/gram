# ResolveChallengeForm

## Example Usage

```typescript
import { ResolveChallengeForm } from "@gram/client/models/components/resolvechallengeform.js";

let value: ResolveChallengeForm = {
  challengeIds: [
    "<value 1>",
  ],
  principalUrn: "<value>",
  resolutionType: "dismissed",
  scope: "<value>",
};
```

## Fields

| Field                                                                                                          | Type                                                                                                           | Required                                                                                                       | Description                                                                                                    |
| -------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| `challengeIds`                                                                                                 | *string*[]                                                                                                     | :heavy_check_mark:                                                                                             | IDs of the challenges in ClickHouse to resolve.                                                                |
| `principalUrn`                                                                                                 | *string*                                                                                                       | :heavy_check_mark:                                                                                             | Principal that was denied.                                                                                     |
| `resolutionType`                                                                                               | [components.ResolveChallengeFormResolutionType](../../models/components/resolvechallengeformresolutiontype.md) | :heavy_check_mark:                                                                                             | How the challenge is being resolved.                                                                           |
| `resourceId`                                                                                                   | *string*                                                                                                       | :heavy_minus_sign:                                                                                             | Resource ID from the challenge.                                                                                |
| `resourceKind`                                                                                                 | *string*                                                                                                       | :heavy_minus_sign:                                                                                             | Resource kind from the challenge.                                                                              |
| `roleSlug`                                                                                                     | *string*                                                                                                       | :heavy_minus_sign:                                                                                             | Role slug to assign (required when resolution_type=role_assigned).                                             |
| `scope`                                                                                                        | *string*                                                                                                       | :heavy_check_mark:                                                                                             | Scope that was denied.                                                                                         |