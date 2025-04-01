# HTTPToolDefinition

## Example Usage

```typescript
import { HTTPToolDefinition } from "@gram/sdk/models/components";

let value: HTTPToolDefinition = {
  apikeyEnvVar: "Quisquam voluptas.",
  bearerEnvVar: "Voluptatibus itaque non.",
  bodySchema: "Qui quis dolore.",
  description: "Perferendis accusamus occaecati laborum quaerat labore.",
  headersSchema: "Aut aperiam velit.",
  httpMethod: "Repellat et est pariatur eum distinctio.",
  id: "Eos error unde tempora.",
  name: "Nemo qui ducimus enim tempore debitis quod.",
  passwordEnvVar: "Sequi est quos.",
  path: "Blanditiis temporibus qui vitae consectetur dolorem reiciendis.",
  pathparamsSchema: "Ut nam dolor provident corporis accusamus.",
  queriesSchema: "Nesciunt vitae sint et in nihil.",
  securityType: "Aut corrupti eos quasi ad fuga voluptatibus.",
  serverEnvVar: "Accusamus optio voluptatibus et repellat.",
  usernameEnvVar: "Quos sint voluptatibus.",
};
```

## Fields

| Field                                                         | Type                                                          | Required                                                      | Description                                                   | Example                                                       |
| ------------------------------------------------------------- | ------------------------------------------------------------- | ------------------------------------------------------------- | ------------------------------------------------------------- | ------------------------------------------------------------- |
| `apikeyEnvVar`                                                | *string*                                                      | :heavy_minus_sign:                                            | Environment variable for API key                              | Sapiente quaerat autem inventore velit.                       |
| `bearerEnvVar`                                                | *string*                                                      | :heavy_minus_sign:                                            | Environment variable for bearer token                         | Error distinctio quia commodi ad quia maiores.                |
| `bodySchema`                                                  | *string*                                                      | :heavy_minus_sign:                                            | JSON schema for request body                                  | Rem necessitatibus aut dignissimos sit.                       |
| `description`                                                 | *string*                                                      | :heavy_check_mark:                                            | Description of the tool                                       | Eveniet voluptatum harum.                                     |
| `headersSchema`                                               | *string*                                                      | :heavy_minus_sign:                                            | JSON schema for headers                                       | Ratione praesentium dolor officiis nostrum earum non.         |
| `httpMethod`                                                  | *string*                                                      | :heavy_check_mark:                                            | HTTP method for the request                                   | Tempora sit repellat qui rem labore ducimus.                  |
| `id`                                                          | *string*                                                      | :heavy_check_mark:                                            | The ID of the HTTP tool                                       | Quo corrupti molestias velit.                                 |
| `name`                                                        | *string*                                                      | :heavy_check_mark:                                            | The name of the tool                                          | Et rerum error aut.                                           |
| `passwordEnvVar`                                              | *string*                                                      | :heavy_minus_sign:                                            | Environment variable for password                             | Ut dicta aliquam iusto sunt.                                  |
| `path`                                                        | *string*                                                      | :heavy_check_mark:                                            | Path for the request                                          | Molestiae eveniet aut in non ea reprehenderit.                |
| `pathparamsSchema`                                            | *string*                                                      | :heavy_minus_sign:                                            | JSON schema for path parameters                               | Soluta nam sed et qui officia.                                |
| `queriesSchema`                                               | *string*                                                      | :heavy_minus_sign:                                            | JSON schema for query parameters                              | Consequuntur aut in praesentium voluptates reprehenderit vel. |
| `securityType`                                                | *string*                                                      | :heavy_check_mark:                                            | Type of security (http:bearer, http:basic, apikey)            | Totam quos fugit sed consequatur qui.                         |
| `serverEnvVar`                                                | *string*                                                      | :heavy_check_mark:                                            | Environment variable for the server URL                       | Voluptatem ullam.                                             |
| `usernameEnvVar`                                              | *string*                                                      | :heavy_minus_sign:                                            | Environment variable for username                             | Quos provident.                                               |