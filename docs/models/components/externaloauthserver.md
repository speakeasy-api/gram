# ExternalOAuthServer

## Example Usage

```typescript
import { ExternalOAuthServer } from "@gram/client/models/components";

let value: ExternalOAuthServer = {
  createdAt: new Date("2025-03-01T16:09:27.293Z"),
  id: "<id>",
  metadata: "<value>",
  projectId: "<id>",
  slug: "<value>",
  updatedAt: new Date("2024-02-12T23:43:49.362Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the external OAuth server was created.                                                   |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the external OAuth server                                                           |
| `metadata`                                                                                    | *any*                                                                                         | :heavy_check_mark:                                                                            | The metadata for the external OAuth server                                                    |
| `projectId`                                                                                   | *string*                                                                                      | :heavy_check_mark:                                                                            | The project ID this external OAuth server belongs to                                          |
| `slug`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | A short url-friendly label that uniquely identifies a resource.                               |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the external OAuth server was last updated.                                              |