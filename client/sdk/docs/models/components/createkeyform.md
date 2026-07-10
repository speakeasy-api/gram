# CreateKeyForm

## Example Usage

```typescript
import { CreateKeyForm } from "@gram/client/models/components/createkeyform.js";

let value: CreateKeyForm = {
  name: "<value>",
  scopes: ["<value 1>", "<value 2>"],
};
```

## Fields

| Field    | Type       | Required           | Description                                            |
| -------- | ---------- | ------------------ | ------------------------------------------------------ |
| `name`   | _string_   | :heavy_check_mark: | The name of the key                                    |
| `scopes` | _string_[] | :heavy_check_mark: | The scopes of the key that determines its permissions. |
