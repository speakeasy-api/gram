# HTTPToolDefinition

## Example Usage

```typescript
import { HTTPToolDefinition } from "@gram/sdk/models/components";

let value: HTTPToolDefinition = {
  createdAt: new Date("2025-03-04T09:26:20.697Z"),
  description:
    "winding oh burly lest notwithstanding viciously curiously swathe a atop",
  httpMethod: "<value>",
  id: "<id>",
  name: "<value>",
  path: "/opt/share",
  securityType: "<value>",
  serverEnvVar: "<value>",
  tags: [
    "<value>",
  ],
  updatedAt: new Date("2023-01-25T17:59:41.729Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `apikeyEnvVar`                                                                                | *string*                                                                                      | :heavy_minus_sign:                                                                            | Environment variable for API key                                                              |
| `bearerEnvVar`                                                                                | *string*                                                                                      | :heavy_minus_sign:                                                                            | Environment variable for bearer token                                                         |
| `bodySchema`                                                                                  | *string*                                                                                      | :heavy_minus_sign:                                                                            | JSON schema for request body                                                                  |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | The creation date of the tool.                                                                |
| `description`                                                                                 | *string*                                                                                      | :heavy_check_mark:                                                                            | Description of the tool                                                                       |
| `headersSchema`                                                                               | *string*                                                                                      | :heavy_minus_sign:                                                                            | JSON schema for headers                                                                       |
| `httpMethod`                                                                                  | *string*                                                                                      | :heavy_check_mark:                                                                            | HTTP method for the request                                                                   |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the HTTP tool                                                                       |
| `name`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | The name of the tool                                                                          |
| `passwordEnvVar`                                                                              | *string*                                                                                      | :heavy_minus_sign:                                                                            | Environment variable for password                                                             |
| `path`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | Path for the request                                                                          |
| `pathparamsSchema`                                                                            | *string*                                                                                      | :heavy_minus_sign:                                                                            | JSON schema for path parameters                                                               |
| `queriesSchema`                                                                               | *string*                                                                                      | :heavy_minus_sign:                                                                            | JSON schema for query parameters                                                              |
| `securityType`                                                                                | *string*                                                                                      | :heavy_check_mark:                                                                            | Type of security (http:bearer, http:basic, apikey)                                            |
| `serverEnvVar`                                                                                | *string*                                                                                      | :heavy_check_mark:                                                                            | Environment variable for the server URL                                                       |
| `tags`                                                                                        | *string*[]                                                                                    | :heavy_check_mark:                                                                            | The tags list for this http tool                                                              |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | The last update date of the tool.                                                             |
| `usernameEnvVar`                                                                              | *string*                                                                                      | :heavy_minus_sign:                                                                            | Environment variable for username                                                             |