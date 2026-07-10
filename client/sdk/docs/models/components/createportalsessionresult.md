# CreatePortalSessionResult

## Example Usage

```typescript
import { CreatePortalSessionResult } from "@gram/client/models/components/createportalsessionresult.js";

let value: CreatePortalSessionResult = {
  token: "<value>",
  url: "https://sardonic-accountability.org/",
};
```

## Fields

| Field                                           | Type                                            | Required                                        | Description                                     |
| ----------------------------------------------- | ----------------------------------------------- | ----------------------------------------------- | ----------------------------------------------- |
| `token`                                         | *string*                                        | :heavy_check_mark:                              | Front-end token for the webhook portal session. |
| `url`                                           | *string*                                        | :heavy_check_mark:                              | URL for the webhook portal session.             |