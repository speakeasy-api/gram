# CreateUserSessionIssuerForm

Form for creating a user_session_issuer.

## Example Usage

```typescript
import { CreateUserSessionIssuerForm } from "@gram/client/models/components/createusersessionissuerform.js";

let value: CreateUserSessionIssuerForm = {
  authnChallengeMode: "chain",
  sessionDurationHours: 262597,
  slug: "<value>",
};
```

## Fields

| Field                  | Type                                                                           | Required           | Description                                                            |
| ---------------------- | ------------------------------------------------------------------------------ | ------------------ | ---------------------------------------------------------------------- |
| `authnChallengeMode`   | [components.AuthnChallengeMode](../../models/components/authnchallengemode.md) | :heavy_check_mark: | How multi-remote authn challenges are presented: chain \| interactive. |
| `sessionDurationHours` | _number_                                                                       | :heavy_check_mark: | Issued user session lifetime, in hours.                                |
| `slug`                 | _string_                                                                       | :heavy_check_mark: | Project-unique slug.                                                   |
