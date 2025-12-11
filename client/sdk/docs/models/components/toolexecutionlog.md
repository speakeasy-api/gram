# ToolExecutionLog

Structured log entry from a tool execution

## Example Usage

```typescript
import { ToolExecutionLog } from "@gram/client/models/components";

let value: ToolExecutionLog = {
  deploymentId: "b491082b-8371-4c9c-817a-9213c50fa722",
  functionId: "195611d1-51e7-4ed1-ac21-c69295aff356",
  id: "0b70892f-2cb8-4291-b7eb-0cc0377f45b8",
  instance: "<value>",
  level: "<value>",
  projectId: "356a7d8c-a598-4a16-8eb8-5d6a890304ef",
  rawLog: "<value>",
  source: "<value>",
  timestamp: new Date("2023-04-04T06:17:08.532Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `attributes`                                                                                  | *string*                                                                                      | :heavy_minus_sign:                                                                            | JSON-encoded log attributes                                                                   |
| `deploymentId`                                                                                | *string*                                                                                      | :heavy_check_mark:                                                                            | Deployment UUID                                                                               |
| `functionId`                                                                                  | *string*                                                                                      | :heavy_check_mark:                                                                            | Function UUID                                                                                 |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | Log entry ID                                                                                  |
| `instance`                                                                                    | *string*                                                                                      | :heavy_check_mark:                                                                            | Instance identifier                                                                           |
| `level`                                                                                       | *string*                                                                                      | :heavy_check_mark:                                                                            | Log level                                                                                     |
| `message`                                                                                     | *string*                                                                                      | :heavy_minus_sign:                                                                            | Parsed log message                                                                            |
| `projectId`                                                                                   | *string*                                                                                      | :heavy_check_mark:                                                                            | Project UUID                                                                                  |
| `rawLog`                                                                                      | *string*                                                                                      | :heavy_check_mark:                                                                            | Raw log message                                                                               |
| `source`                                                                                      | *string*                                                                                      | :heavy_check_mark:                                                                            | Log source                                                                                    |
| `timestamp`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | Timestamp of the log entry                                                                    |