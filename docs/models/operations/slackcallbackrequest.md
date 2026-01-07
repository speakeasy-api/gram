# SlackCallbackRequest

## Example Usage

```typescript
import { SlackCallbackRequest } from "@gram/client/models/operations";

let value: SlackCallbackRequest = {
  state: "North Carolina",
  code: "<value>",
};
```

## Fields

| Field                                 | Type                                  | Required                              | Description                           |
| ------------------------------------- | ------------------------------------- | ------------------------------------- | ------------------------------------- |
| `state`                               | *string*                              | :heavy_check_mark:                    | The state parameter from the callback |
| `code`                                | *string*                              | :heavy_check_mark:                    | The code parameter from the callback  |