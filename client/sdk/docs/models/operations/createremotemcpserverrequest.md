# CreateRemoteMcpServerRequest

## Example Usage

```typescript
import { CreateRemoteMcpServerRequest } from "@gram/client/models/operations/createremotemcpserver.js";

let value: CreateRemoteMcpServerRequest = {
  createServerForm: {
    headers: [],
    transportType: "<value>",
    url: "https://pessimistic-eternity.net",
  },
};
```

## Fields

| Field              | Type                                                                       | Required           | Description    |
| ------------------ | -------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramSession`      | _string_                                                                   | :heavy_minus_sign: | Session header |
| `gramKey`          | _string_                                                                   | :heavy_minus_sign: | API Key header |
| `gramProject`      | _string_                                                                   | :heavy_minus_sign: | project header |
| `createServerForm` | [components.CreateServerForm](../../models/components/createserverform.md) | :heavy_check_mark: | N/A            |
