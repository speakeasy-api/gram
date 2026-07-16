# SyncSkillException

A distributed skill the caller could not apply locally.


## Fields

| Field                                                  | Type                                                   | Required                                               | Description                                            |
| ------------------------------------------------------ | ------------------------------------------------------ | ------------------------------------------------------ | ------------------------------------------------------ |
| `Name`                                                 | `string`                                               | :heavy_check_mark:                                     | The normalized skill name.                             |
| `Status`                                               | [components.Status](../../models/components/status.md) | :heavy_check_mark:                                     | Why the distributed skill was not applied.             |