# DiscoverRemoteMcpProtectedResourceMetadataRequest

## Example Usage

```typescript
import { DiscoverRemoteMcpProtectedResourceMetadataRequest } from "@gram/client/models/operations/discoverremotemcpprotectedresourcemetadata.js";

let value: DiscoverRemoteMcpProtectedResourceMetadataRequest = {
  discoverProtectedResourceMetadataRequestBody: {
    remoteMcpServerId: "749a782e-aebe-47d0-ba3f-c849bb7ee579",
  },
};
```

## Fields

| Field                                                                                                                              | Type                                                                                                                               | Required                                                                                                                           | Description                                                                                                                        |
| ---------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| `gramSession`                                                                                                                      | *string*                                                                                                                           | :heavy_minus_sign:                                                                                                                 | Session header                                                                                                                     |
| `gramKey`                                                                                                                          | *string*                                                                                                                           | :heavy_minus_sign:                                                                                                                 | API Key header                                                                                                                     |
| `gramProject`                                                                                                                      | *string*                                                                                                                           | :heavy_minus_sign:                                                                                                                 | project header                                                                                                                     |
| `discoverProtectedResourceMetadataRequestBody`                                                                                     | [components.DiscoverProtectedResourceMetadataRequestBody](../../models/components/discoverprotectedresourcemetadatarequestbody.md) | :heavy_check_mark:                                                                                                                 | N/A                                                                                                                                |