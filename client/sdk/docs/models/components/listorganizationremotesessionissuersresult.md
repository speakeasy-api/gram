# ListOrganizationRemoteSessionIssuersResult

Result type for the organization-administrator issuer listing — organizational and project-specific issuers across the org.

## Example Usage

```typescript
import { ListOrganizationRemoteSessionIssuersResult } from "@gram/client/models/components/listorganizationremotesessionissuersresult.js";

let value: ListOrganizationRemoteSessionIssuersResult = {
  items: [
    {
      clientCount: 803004,
      issuer: {
        clientIdMetadataDocumentSupported: false,
        createdAt: new Date("2024-06-24T07:44:21.070Z"),
        id: "a2b75a6a-51c2-4c82-828d-f80ac522fa83",
        issuer: "jcb",
        oidc: true,
        organizationId: "<id>",
        passthrough: true,
        projectId: "<id>",
        slug: "<value>",
        updatedAt: new Date("2025-06-13T18:35:12.908Z"),
      },
    },
  ],
};
```

## Fields

| Field        | Type                                                                                                       | Required           | Description                                     |
| ------------ | ---------------------------------------------------------------------------------------------------------- | ------------------ | ----------------------------------------------- |
| `items`      | [components.OrganizationRemoteSessionIssuer](../../models/components/organizationremotesessionissuer.md)[] | :heavy_check_mark: | N/A                                             |
| `nextCursor` | _string_                                                                                                   | :heavy_minus_sign: | Cursor for the next page; empty when exhausted. |
