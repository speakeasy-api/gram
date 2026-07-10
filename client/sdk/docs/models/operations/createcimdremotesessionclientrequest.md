# CreateCimdRemoteSessionClientRequest

## Example Usage

```typescript
import { CreateCimdRemoteSessionClientRequest } from "@gram/client/models/operations/createcimdremotesessionclient.js";

let value: CreateCimdRemoteSessionClientRequest = {
  createCimdForm: {
    remoteSessionIssuerId: "b77d3e41-a64f-4dee-ac2b-327316a50c2a",
  },
};
```

## Fields

| Field            | Type                                                                   | Required           | Description    |
| ---------------- | ---------------------------------------------------------------------- | ------------------ | -------------- |
| `gramSession`    | _string_                                                               | :heavy_minus_sign: | Session header |
| `gramKey`        | _string_                                                               | :heavy_minus_sign: | API Key header |
| `gramProject`    | _string_                                                               | :heavy_minus_sign: | project header |
| `createCimdForm` | [components.CreateCimdForm](../../models/components/createcimdform.md) | :heavy_check_mark: | N/A            |
