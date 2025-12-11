# AddExternalMCPForm

## Example Usage

```typescript
import { AddExternalMCPForm } from "@gram/client/models/components";

let value: AddExternalMCPForm = {
  name: "ai.exa/exa",
  registryId: "ef1d375d-6fc0-4c75-91e1-a03ee8652a8f",
  slug: "<value>",
};
```

## Fields

| Field                                                                 | Type                                                                  | Required                                                              | Description                                                           | Example                                                               |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `name`                                                                | *string*                                                              | :heavy_check_mark:                                                    | The reverse-DNS name of the external MCP server (e.g., 'ai.exa/exa'). | ai.exa/exa                                                            |
| `registryId`                                                          | *string*                                                              | :heavy_check_mark:                                                    | The ID of the MCP registry the server is from.                        |                                                                       |
| `slug`                                                                | *string*                                                              | :heavy_check_mark:                                                    | A short url-friendly label that uniquely identifies a resource.       |                                                                       |