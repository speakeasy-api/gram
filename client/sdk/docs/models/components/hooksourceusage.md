# HookSourceUsage

Hook source usage statistics

## Example Usage

```typescript
import { HookSourceUsage } from "@gram/client/models/components/hooksourceusage.js";

let value: HookSourceUsage = {
  eventCount: 230267,
  source: "<value>",
};
```

## Fields

| Field                                          | Type                                           | Required                                       | Description                                    |
| ---------------------------------------------- | ---------------------------------------------- | ---------------------------------------------- | ---------------------------------------------- |
| `eventCount`                                   | *number*                                       | :heavy_check_mark:                             | Total hook events for this source              |
| `source`                                       | *string*                                       | :heavy_check_mark:                             | Hook source (from attributes.gram.hook.source) |