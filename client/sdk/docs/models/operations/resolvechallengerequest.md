# ResolveChallengeRequest

## Example Usage

```typescript
import { ResolveChallengeRequest } from "@gram/client/models/operations/resolvechallenge.js";

let value: ResolveChallengeRequest = {
  resolveChallengeForm: {
    challengeIds: ["<value 1>"],
    principalUrn: "<value>",
    resolutionType: "dismissed",
    scope: "<value>",
  },
};
```

## Fields

| Field                  | Type                                                                               | Required           | Description    |
| ---------------------- | ---------------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramKey`              | _string_                                                                           | :heavy_minus_sign: | API Key header |
| `gramSession`          | _string_                                                                           | :heavy_minus_sign: | Session header |
| `resolveChallengeForm` | [components.ResolveChallengeForm](../../models/components/resolvechallengeform.md) | :heavy_check_mark: | N/A            |
