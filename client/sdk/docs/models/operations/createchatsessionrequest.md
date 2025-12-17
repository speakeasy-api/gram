# CreateChatSessionRequest

## Example Usage

```typescript
import { CreateChatSessionRequest } from "@gram/client/models/operations";

let value: CreateChatSessionRequest = {
  createRequestBody: {},
};
```

## Fields

| Field                                                                        | Type                                                                         | Required                                                                     | Description                                                                  |
| ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| `gramKey`                                                                    | *string*                                                                     | :heavy_minus_sign:                                                           | API Key header                                                               |
| `gramProject`                                                                | *string*                                                                     | :heavy_minus_sign:                                                           | project header                                                               |
| `createRequestBody`                                                          | [components.CreateRequestBody](../../models/components/createrequestbody.md) | :heavy_check_mark:                                                           | N/A                                                                          |