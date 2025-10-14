# ListVariationsResult

## Example Usage

```typescript
import { ListVariationsResult } from "@gram/client/models/components";

let value: ListVariationsResult = {
  variations: [
    {
      createdAt: "1708604588094",
      groupId: "<id>",
      id: "<id>",
      srcToolName: "<value>",
      srcToolUrn: "<value>",
      updatedAt: "1735676357006",
    },
  ],
};
```

## Fields

| Field                                                                  | Type                                                                   | Required                                                               | Description                                                            |
| ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `variations`                                                           | [components.ToolVariation](../../models/components/toolvariation.md)[] | :heavy_check_mark:                                                     | N/A                                                                    |