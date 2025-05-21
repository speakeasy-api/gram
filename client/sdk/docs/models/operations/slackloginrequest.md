# SlackLoginRequest

## Example Usage

```typescript
import { SlackLoginRequest } from "@gram/client/models/operations";

let value: SlackLoginRequest = {
  projectSlug: "<value>",
};
```

## Fields

| Field                                | Type                                 | Required                             | Description                          |
| ------------------------------------ | ------------------------------------ | ------------------------------------ | ------------------------------------ |
| `projectSlug`                        | *string*                             | :heavy_check_mark:                   | N/A                                  |
| `returnUrl`                          | *string*                             | :heavy_minus_sign:                   | The dashboard location to return too |
| `gramSession`                        | *string*                             | :heavy_minus_sign:                   | Session header                       |