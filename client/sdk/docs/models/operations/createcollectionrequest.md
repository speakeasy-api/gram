# CreateCollectionRequest

## Example Usage

```typescript
import { CreateCollectionRequest } from "@gram/client/models/operations/createcollection.js";

let value: CreateCollectionRequest = {
  createRequestBody2: {
    mcpRegistryNamespace: "<value>",
    name: "<value>",
    slug: "<value>",
  },
};
```

## Fields

| Field                                                                          | Type                                                                           | Required                                                                       | Description                                                                    |
| ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ |
| `gramSession`                                                                  | *string*                                                                       | :heavy_minus_sign:                                                             | Session header                                                                 |
| `gramKey`                                                                      | *string*                                                                       | :heavy_minus_sign:                                                             | API Key header                                                                 |
| `createRequestBody2`                                                           | [components.CreateRequestBody2](../../models/components/createrequestbody2.md) | :heavy_check_mark:                                                             | N/A                                                                            |