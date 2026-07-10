# GenerateTitleRequestBody

## Example Usage

```typescript
import { GenerateTitleRequestBody } from "@gram/client/models/components/generatetitlerequestbody.js";

let value: GenerateTitleRequestBody = {
  id: "<id>",
};
```

## Fields

| Field                                                                                                                                              | Type                                                                                                                                               | Required                                                                                                                                           | Description                                                                                                                                        |
| -------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| `id`                                                                                                                                               | *string*                                                                                                                                           | :heavy_check_mark:                                                                                                                                 | The ID of the chat                                                                                                                                 |
| `title`                                                                                                                                            | *string*                                                                                                                                           | :heavy_minus_sign:                                                                                                                                 | When present, sets the chat's title manually (empty string resets to auto-generated). When omitted, the current title is returned without changes. |