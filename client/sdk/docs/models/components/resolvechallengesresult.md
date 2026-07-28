# ResolveChallengesResult

## Example Usage

```typescript
import { ResolveChallengesResult } from "@gram/client/models/components/resolvechallengesresult.js";

let value: ResolveChallengesResult = {
  resolutions: [
    {
      challengeId: "<id>",
      createdAt: new Date("2025-10-30T20:33:21.752Z"),
      id: "<id>",
      organizationId: "<id>",
      principalUrn: "<value>",
      resolutionType: "role_assigned",
      resolvedBy: "<value>",
      scope: "<value>",
    },
  ],
};
```

## Fields

| Field         | Type                                                                               | Required           | Description                     |
| ------------- | ---------------------------------------------------------------------------------- | ------------------ | ------------------------------- |
| `resolutions` | [components.ChallengeResolution](../../models/components/challengeresolution.md)[] | :heavy_check_mark: | The created resolution records. |
