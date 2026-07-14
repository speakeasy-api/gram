# RedeemResponseBody

## Example Usage

```typescript
import { RedeemResponseBody } from "@gram/client/models/components/redeemresponsebody.js";

let value: RedeemResponseBody = {
  accessToken: "<value>",
  projectSlug: "<value>",
  userEmail: "<value>",
};
```

## Fields

| Field         | Type     | Required           | Description                                                                       |
| ------------- | -------- | ------------------ | --------------------------------------------------------------------------------- |
| `accessToken` | _string_ | :heavy_check_mark: | The raw gram\_ API key, carrying the [agent,hooks] scopes. Returned exactly once. |
| `projectSlug` | _string_ | :heavy_check_mark: | Slug of the project the key is scoped to.                                         |
| `userEmail`   | _string_ | :heavy_check_mark: | Email of the user the key was minted for.                                         |
