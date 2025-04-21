# GetInstanceRequest


## Fields

| Field                               | Type                                | Required                            | Description                         |
| ----------------------------------- | ----------------------------------- | ----------------------------------- | ----------------------------------- |
| `toolset_slug`                      | *str*                               | :heavy_check_mark:                  | The slug of the toolset to load     |
| `environment_slug`                  | *Optional[str]*                     | :heavy_minus_sign:                  | The slug of the environment to load |
| `gram_session`                      | *Optional[str]*                     | :heavy_minus_sign:                  | Session header                      |
| `gram_project`                      | *Optional[str]*                     | :heavy_minus_sign:                  | project header                      |
| `gram_key`                          | *Optional[str]*                     | :heavy_minus_sign:                  | API Key header                      |