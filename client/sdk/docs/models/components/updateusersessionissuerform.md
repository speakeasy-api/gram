# UpdateUserSessionIssuerForm

Form for updating a user_session_issuer. All non-id fields are optional patches.

## Example Usage

```typescript
import { UpdateUserSessionIssuerForm } from "@gram/client/models/components/updateusersessionissuerform.js";

let value: UpdateUserSessionIssuerForm = {
  id: "fa43deaa-68f4-40e9-8be5-34eef58c620c",
};
```

## Fields

| Field                  | Type                                                                                                                                 | Required           | Description                             |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------------------ | ------------------ | --------------------------------------- |
| `authnChallengeMode`   | [components.UpdateUserSessionIssuerFormAuthnChallengeMode](../../models/components/updateusersessionissuerformauthnchallengemode.md) | :heavy_minus_sign: | chain \| interactive.                   |
| `id`                   | _string_                                                                                                                             | :heavy_check_mark: | The user_session_issuer id.             |
| `sessionDurationHours` | _number_                                                                                                                             | :heavy_minus_sign: | Issued user session lifetime, in hours. |
| `slug`                 | _string_                                                                                                                             | :heavy_minus_sign: | Rename the slug.                        |
