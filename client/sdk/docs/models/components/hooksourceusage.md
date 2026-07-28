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

| Field        | Type     | Required           | Description                                    |
| ------------ | -------- | ------------------ | ---------------------------------------------- |
| `eventCount` | _number_ | :heavy_check_mark: | Total hook events for this source              |
| `source`     | _string_ | :heavy_check_mark: | Hook source (from attributes.gram.hook.source) |
