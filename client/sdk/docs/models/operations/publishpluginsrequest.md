# PublishPluginsRequest

## Example Usage

```typescript
import { PublishPluginsRequest } from "@gram/client/models/operations/publishplugins.js";

let value: PublishPluginsRequest = {
  publishPluginsRequestBody: {},
};
```

## Fields

| Field                                                                                        | Type                                                                                         | Required                                                                                     | Description                                                                                  |
| -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| `gramSession`                                                                                | *string*                                                                                     | :heavy_minus_sign:                                                                           | Session header                                                                               |
| `gramProject`                                                                                | *string*                                                                                     | :heavy_minus_sign:                                                                           | project header                                                                               |
| `publishPluginsRequestBody`                                                                  | [components.PublishPluginsRequestBody](../../models/components/publishpluginsrequestbody.md) | :heavy_check_mark:                                                                           | N/A                                                                                          |