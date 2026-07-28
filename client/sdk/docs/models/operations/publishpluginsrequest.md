# PublishPluginsRequest

## Example Usage

```typescript
import { PublishPluginsRequest } from "@gram/client/models/operations/publishplugins.js";

let value: PublishPluginsRequest = {
  publishPluginsRequestBody: {},
};
```

## Fields

| Field                       | Type                                                                                         | Required           | Description    |
| --------------------------- | -------------------------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramSession`               | _string_                                                                                     | :heavy_minus_sign: | Session header |
| `gramProject`               | _string_                                                                                     | :heavy_minus_sign: | project header |
| `publishPluginsRequestBody` | [components.PublishPluginsRequestBody](../../models/components/publishpluginsrequestbody.md) | :heavy_check_mark: | N/A            |
