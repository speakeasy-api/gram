# HookIngestSession

Agent session and turn identity, independent of provider naming.

## Example Usage

```typescript
import { HookIngestSession } from "@gram/client/models/components/hookingestsession.js";

let value: HookIngestSession = {};
```

## Fields

| Field                                           | Type                                            | Required                                        | Description                                     |
| ----------------------------------------------- | ----------------------------------------------- | ----------------------------------------------- | ----------------------------------------------- |
| `cwd`                                           | *string*                                        | :heavy_minus_sign:                              | Current working directory when the event fired. |
| `id`                                            | *string*                                        | :heavy_minus_sign:                              | Stable conversation or session identifier.      |
| `model`                                         | *string*                                        | :heavy_minus_sign:                              | Model identifier reported by the local agent.   |
| `turnId`                                        | *string*                                        | :heavy_minus_sign:                              | Generation, request, or turn identifier.        |