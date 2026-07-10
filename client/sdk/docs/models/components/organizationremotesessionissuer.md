# OrganizationRemoteSessionIssuer

An organization-administrator view of a remote_session_issuer: the issuer plus its associated client count and (for project-specific issuers) the owning project's name.

## Example Usage

```typescript
import { OrganizationRemoteSessionIssuer } from "@gram/client/models/components/organizationremotesessionissuer.js";

let value: OrganizationRemoteSessionIssuer = {
  clientCount: 686231,
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
};
```

## Fields

| Field                                                                                              | Type                                                                                               | Required                                                                                           | Description                                                                                        |
| -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| `clientCount`                                                                                      | *number*                                                                                           | :heavy_check_mark:                                                                                 | Number of non-deleted remote_session_clients registered with this issuer.                          |
| `issuer`                                                                                           | [components.RemoteSessionIssuer](../../models/components/remotesessionissuer.md)                   | :heavy_check_mark:                                                                                 | A remote_session_issuer record — upstream Authorization Server identity that Gram speaks OAuth to. |
| `projectName`                                                                                      | *string*                                                                                           | :heavy_minus_sign:                                                                                 | The owning project's name. Empty for organizational (project_id NULL) issuers.                     |