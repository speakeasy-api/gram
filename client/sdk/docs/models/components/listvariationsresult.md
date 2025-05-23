# ListVariationsResult

## Example Usage

```typescript
import { ListVariationsResult } from "@gram/client/models/components";

let value: ListVariationsResult = {
  variations: [
    {
      createdAt: "1735496867052",
      groupId: "<id>",
      id: "<id>",
      srcToolName: "<value>",
      updatedAt: "1735615394347",
    },
  ],
};
```

## Fields

| Field                                                                  | Type                                                                   | Required                                                               | Description                                                            |
| ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `variations`                                                           | [components.ToolVariation](../../models/components/toolvariation.md)[] | :heavy_check_mark:                                                     | N/A                                                                    |