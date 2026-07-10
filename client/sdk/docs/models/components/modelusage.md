# ModelUsage

Model usage statistics

## Example Usage

```typescript
import { ModelUsage } from "@gram/client/models/components/modelusage.js";

let value: ModelUsage = {
  count: 275934,
  name: "<value>",
};
```

## Fields

| Field                | Type                 | Required             | Description          |
| -------------------- | -------------------- | -------------------- | -------------------- |
| `count`              | *number*             | :heavy_check_mark:   | Number of times used |
| `name`               | *string*             | :heavy_check_mark:   | Model name           |