# ListRemoteSessionIssuersResponse

## Example Usage

```typescript
import { ListRemoteSessionIssuersResponse } from "@gram/client/models/operations/listremotesessionissuers.js";

let value: ListRemoteSessionIssuersResponse = {
  result: {
    items: [
      {
        clientIdMetadataDocumentSupported: true,
        createdAt: new Date("2025-10-04T16:28:03.663Z"),
        id: "c14d437d-8724-45f9-9f17-851d216c12aa",
        issuer: "jcb",
        oidc: true,
        organizationId: "<id>",
        passthrough: true,
        projectId: "<id>",
        slug: "<value>",
        updatedAt: new Date("2026-12-21T08:12:35.833Z"),
      },
    ],
  },
};
```

## Fields

| Field                                                                                                  | Type                                                                                                   | Required                                                                                               | Description                                                                                            |
| ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ |
| `result`                                                                                               | [components.ListRemoteSessionIssuersResult](../../models/components/listremotesessionissuersresult.md) | :heavy_check_mark:                                                                                     | N/A                                                                                                    |