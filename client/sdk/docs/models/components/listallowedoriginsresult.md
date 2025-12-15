# ListAllowedOriginsResult

## Example Usage

```typescript
import { ListAllowedOriginsResult } from "@gram/client/models/components";

let value: ListAllowedOriginsResult = {
  allowedOrigins: [
    {
      createdAt: new Date("2024-06-06T17:00:10.839Z"),
      id: "<id>",
      origin: "<value>",
      projectId: "<id>",
      status: "<value>",
      updatedAt: new Date("2025-11-03T17:27:17.996Z"),
    },
  ],
};
```

## Fields

| Field                                                                  | Type                                                                   | Required                                                               | Description                                                            |
| ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `allowedOrigins`                                                       | [components.AllowedOrigin](../../models/components/allowedorigin.md)[] | :heavy_check_mark:                                                     | The list of allowed origins                                            |