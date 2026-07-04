# HookIngestSession

Agent session and turn identity, independent of provider naming.

## Fields

| Field    | Type      | Required           | Description                                     |
| -------- | --------- | ------------------ | ----------------------------------------------- |
| `Cwd`    | `*string` | :heavy_minus_sign: | Current working directory when the event fired. |
| `ID`     | `*string` | :heavy_minus_sign: | Stable conversation or session identifier.      |
| `Model`  | `*string` | :heavy_minus_sign: | Model identifier reported by the local agent.   |
| `TurnID` | `*string` | :heavy_minus_sign: | Generation, request, or turn identifier.        |
