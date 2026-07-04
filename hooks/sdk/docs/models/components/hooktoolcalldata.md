# HookToolCallData

Tool call feature payload.

## Fields

| Field            | Type       | Required           | Description                                              |
| ---------------- | ---------- | ------------------ | -------------------------------------------------------- |
| `DurationMs`     | `*float64` | :heavy_minus_sign: | Tool execution duration in milliseconds, when reported.  |
| `Error`          | `any`      | :heavy_minus_sign: | Tool error payload.                                      |
| `ID`             | `*string`  | :heavy_minus_sign: | Provider tool call identifier.                           |
| `Input`          | `any`      | :heavy_minus_sign: | Tool input payload.                                      |
| `IsInterrupt`    | `*bool`    | :heavy_minus_sign: | Whether the failure was caused by user interruption.     |
| `Name`           | `*string`  | :heavy_minus_sign: | Tool name.                                               |
| `Output`         | `any`      | :heavy_minus_sign: | Tool output payload.                                     |
| `PermissionType` | `*string`  | :heavy_minus_sign: | Permission type requested by the agent, when applicable. |
| `Status`         | `*string`  | :heavy_minus_sign: | Provider-reported tool call status, when available.      |
