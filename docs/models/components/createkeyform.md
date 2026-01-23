# CreateKeyForm

## Example Usage

```typescript
import { CreateKeyForm } from "@gram/client/models/components";

let value: CreateKeyForm = {
  name: "<value>",
  scopes: [
    "<value 1>",
    "<value 2>",
  ],
};
```

## Fields

| Field                                                  | Type                                                   | Required                                               | Description                                            |
| ------------------------------------------------------ | ------------------------------------------------------ | ------------------------------------------------------ | ------------------------------------------------------ |
| `name`                                                 | *string*                                               | :heavy_check_mark:                                     | The name of the key                                    |
| `scopes`                                               | *string*[]                                             | :heavy_check_mark:                                     | The scopes of the key that determines its permissions. |