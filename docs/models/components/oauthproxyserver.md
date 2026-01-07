# OAuthProxyServer

## Example Usage

```typescript
import { OAuthProxyServer } from "@gram/client/models/components";

let value: OAuthProxyServer = {
  createdAt: new Date("2025-01-25T13:27:19.398Z"),
  id: "<id>",
  projectId: "<id>",
  slug: "<value>",
  updatedAt: new Date("2025-03-18T13:32:08.277Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the OAuth proxy server was created.                                                      |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the OAuth proxy server                                                              |
| `oauthProxyProviders`                                                                         | [components.OAuthProxyProvider](../../models/components/oauthproxyprovider.md)[]              | :heavy_minus_sign:                                                                            | The OAuth proxy providers for this server                                                     |
| `projectId`                                                                                   | *string*                                                                                      | :heavy_check_mark:                                                                            | The project ID this OAuth proxy server belongs to                                             |
| `slug`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | A short url-friendly label that uniquely identifies a resource.                               |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the OAuth proxy server was last updated.                                                 |