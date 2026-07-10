# ListCustomDomainMcpEndpointsResult

Result of listing the MCP endpoints registered under an organization's custom domain.

## Example Usage

```typescript
import { ListCustomDomainMcpEndpointsResult } from "@gram/client/models/components/listcustomdomainmcpendpointsresult.js";

let value: ListCustomDomainMcpEndpointsResult = {
  mcpEndpoints: [
    {
      id: "68ef5078-51bb-49a1-93de-86d661aa20e6",
      mcpServerId: "e9c7be74-de83-48b5-b7d4-285be6de0ae6",
      projectId: "dc0459eb-c1d9-495b-b5cf-5d8f46a2c1a1",
      projectName: "<value>",
      projectSlug: "<value>",
      slug: "<value>",
    },
  ],
};
```

## Fields

| Field                                                                                      | Type                                                                                       | Required                                                                                   | Description                                                                                |
| ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------ |
| `mcpEndpoints`                                                                             | [components.CustomDomainMcpEndpoint](../../models/components/customdomainmcpendpoint.md)[] | :heavy_check_mark:                                                                         | N/A                                                                                        |