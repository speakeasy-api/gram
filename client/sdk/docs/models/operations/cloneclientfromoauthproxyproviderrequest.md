# CloneClientFromOAuthProxyProviderRequest

## Example Usage

```typescript
import { CloneClientFromOAuthProxyProviderRequest } from "@gram/client/models/operations/cloneclientfromoauthproxyprovider.js";

let value: CloneClientFromOAuthProxyProviderRequest = {
  cloneClientFromOAuthProxyProviderForm: {
    oauthProxyProviderId: "a0f9780a-8a2b-4ad9-b80f-d5e505ba7c0c",
    remoteSessionIssuerId: "42f5cb7d-4649-45e3-ba5c-8a1a2c5b942c",
  },
};
```

## Fields

| Field                                   | Type                                                                                                                 | Required           | Description    |
| --------------------------------------- | -------------------------------------------------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramSession`                           | _string_                                                                                                             | :heavy_minus_sign: | Session header |
| `gramKey`                               | _string_                                                                                                             | :heavy_minus_sign: | API Key header |
| `gramProject`                           | _string_                                                                                                             | :heavy_minus_sign: | project header |
| `cloneClientFromOAuthProxyProviderForm` | [components.CloneClientFromOAuthProxyProviderForm](../../models/components/cloneclientfromoauthproxyproviderform.md) | :heavy_check_mark: | N/A            |
