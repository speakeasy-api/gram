# UpdateMcpEndpointForm

Form for updating an MCP endpoint. This is a full-record replace: the custom_domain_id field omitted from the request becomes null on the stored record. Platform-domain endpoint slugs (no custom_domain_id) must be prefixed with the organization slug.

## Example Usage

```typescript
import { UpdateMcpEndpointForm } from "@gram/client/models/components/updatemcpendpointform.js";

let value: UpdateMcpEndpointForm = {
  id: "7e0df200-ce9b-4c99-a4a7-a52a4fed8c14",
  mcpServerId: "6aaa4800-a9c9-4991-aea3-35bdecc47f07",
  slug: "<value>",
};
```

## Fields

| Field            | Type     | Required           | Description                                                                                                                                                                              |
| ---------------- | -------- | ------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `customDomainId` | _string_ | :heavy_minus_sign: | The ID of the custom domain to register the endpoint slug under. Omit to move the endpoint to a platform domain.                                                                         |
| `id`             | _string_ | :heavy_check_mark: | The ID of the MCP endpoint to update                                                                                                                                                     |
| `mcpServerId`    | _string_ | :heavy_check_mark: | The ID of the MCP server this endpoint addresses                                                                                                                                         |
| `slug`           | _string_ | :heavy_check_mark: | A url-friendly label (up to 128 characters) that addresses an MCP server through a slug-based URL. Platform-domain slugs (no custom domain) must be prefixed with the organization slug. |
