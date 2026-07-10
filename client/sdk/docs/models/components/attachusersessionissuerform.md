# AttachUserSessionIssuerForm

Form for attaching a user_session_issuer to a remote_session_client via the join table.

## Example Usage

```typescript
import { AttachUserSessionIssuerForm } from "@gram/client/models/components/attachusersessionissuerform.js";

let value: AttachUserSessionIssuerForm = {
  id: "a2c216ba-3c87-4846-8c31-c624fe63675f",
  userSessionIssuerId: "bdd2056b-d3cf-4d47-b92a-f1f2a2893ec8",
};
```

## Fields

| Field                              | Type                               | Required                           | Description                        |
| ---------------------------------- | ---------------------------------- | ---------------------------------- | ---------------------------------- |
| `id`                               | *string*                           | :heavy_check_mark:                 | The remote_session_client id.      |
| `userSessionIssuerId`              | *string*                           | :heavy_check_mark:                 | The user_session_issuer to attach. |