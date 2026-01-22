# ListProvidersData


## Fields

| Field                                    | Type                                     | Required                                 | Description                              | Example                                  |
| ---------------------------------------- | ---------------------------------------- | ---------------------------------------- | ---------------------------------------- | ---------------------------------------- |
| `Name`                                   | *string*                                 | :heavy_check_mark:                       | Display name of the provider             | OpenAI                                   |
| `Slug`                                   | *string*                                 | :heavy_check_mark:                       | URL-friendly identifier for the provider | openai                                   |
| `PrivacyPolicyURL`                       | *string*                                 | :heavy_check_mark:                       | URL to the provider's privacy policy     | https://openai.com/privacy               |
| `TermsOfServiceURL`                      | **string*                                | :heavy_minus_sign:                       | URL to the provider's terms of service   | https://openai.com/terms                 |
| `StatusPageURL`                          | **string*                                | :heavy_minus_sign:                       | URL to the provider's status page        | https://status.openai.com                |