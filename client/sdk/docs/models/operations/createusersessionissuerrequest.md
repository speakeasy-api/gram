# CreateUserSessionIssuerRequest

## Example Usage

```typescript
import { CreateUserSessionIssuerRequest } from "@gram/client/models/operations/createusersessionissuer.js";

let value: CreateUserSessionIssuerRequest = {
  createUserSessionIssuerForm: {
    authnChallengeMode: "chain",
    sessionDurationHours: 770412,
    slug: "<value>",
  },
};
```

## Fields

| Field                         | Type                                                                                             | Required           | Description    |
| ----------------------------- | ------------------------------------------------------------------------------------------------ | ------------------ | -------------- |
| `gramSession`                 | _string_                                                                                         | :heavy_minus_sign: | Session header |
| `gramKey`                     | _string_                                                                                         | :heavy_minus_sign: | API Key header |
| `gramProject`                 | _string_                                                                                         | :heavy_minus_sign: | project header |
| `createUserSessionIssuerForm` | [components.CreateUserSessionIssuerForm](../../models/components/createusersessionissuerform.md) | :heavy_check_mark: | N/A            |
