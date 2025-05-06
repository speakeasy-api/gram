# IntegrationsNumberGetRequest

## Example Usage

```typescript
import { IntegrationsNumberGetRequest } from "@gram/client/models/operations";

let value: IntegrationsNumberGetRequest = {};
```

## Fields

| Field                                                          | Type                                                           | Required                                                       | Description                                                    |
| -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- |
| `id`                                                           | *string*                                                       | :heavy_minus_sign:                                             | The ID of the integration to get (refers to a package id).     |
| `name`                                                         | *string*                                                       | :heavy_minus_sign:                                             | The name of the integration to get (refers to a package name). |
| `gramSession`                                                  | *string*                                                       | :heavy_minus_sign:                                             | Session header                                                 |
| `gramProject`                                                  | *string*                                                       | :heavy_minus_sign:                                             | project header                                                 |