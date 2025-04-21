# GetInstanceResult


## Fields

| Field                                                              | Type                                                               | Required                                                           | Description                                                        |
| ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ |
| `description`                                                      | *Optional[str]*                                                    | :heavy_minus_sign:                                                 | The description of the toolset                                     |
| `environment`                                                      | [models.Environment](../models/environment.md)                     | :heavy_check_mark:                                                 | Model representing an environment                                  |
| `name`                                                             | *str*                                                              | :heavy_check_mark:                                                 | The name of the toolset                                            |
| `relevant_environment_variables`                                   | List[*str*]                                                        | :heavy_minus_sign:                                                 | The environment variables that are relevant to the toolset         |
| `tools`                                                            | List[[models.HTTPToolDefinition](../models/httptooldefinition.md)] | :heavy_check_mark:                                                 | The list of tools                                                  |