# CreateMcpEndpointForm

Form for creating a new MCP endpoint. Platform-domain endpoint slugs (no custom_domain_id) must be prefixed with the organization slug.

## Example Usage

```typescript
import { CreateMcpEndpointForm } from "@gram/client/models/components/createmcpendpointform.js";

let value: CreateMcpEndpointForm = {
  mcpServerId: "5b6a4a6c-ff06-4d4f-bafc-f40c4856d43c",
  slug: "<value>",
};
```

## Fields

| Field            | Type     | Required           | Description                                                                                                                                                                              |
| ---------------- | -------- | ------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `customDomainId` | _string_ | :heavy_minus_sign: | The ID of the custom domain to register the endpoint slug under. Omit for a platform-domain endpoint.                                                                                    |
| `mcpServerId`    | _string_ | :heavy_check_mark: | The ID of the MCP server this endpoint addresses                                                                                                                                         |
| `slug`           | _string_ | :heavy_check_mark: | A url-friendly label (up to 128 characters) that addresses an MCP server through a slug-based URL. Platform-domain slugs (no custom domain) must be prefixed with the organization slug. |
