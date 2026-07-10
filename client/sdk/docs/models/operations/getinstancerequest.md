# GetInstanceRequest

## Example Usage

```typescript
import { GetInstanceRequest } from "@gram/client/models/operations/getinstance.js";

let value: GetInstanceRequest = {
  toolsetSlug: "<value>",
};
```

## Fields

| Field             | Type     | Required           | Description                     |
| ----------------- | -------- | ------------------ | ------------------------------- |
| `toolsetSlug`     | _string_ | :heavy_check_mark: | The slug of the toolset to load |
| `gramSession`     | _string_ | :heavy_minus_sign: | Session header                  |
| `gramProject`     | _string_ | :heavy_minus_sign: | project header                  |
| `gramKey`         | _string_ | :heavy_minus_sign: | API Key header                  |
| `gramChatSession` | _string_ | :heavy_minus_sign: | Chat Sessions token header      |
