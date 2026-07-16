# Skills

## Overview

Manage project skills and their immutable versions. Methods are gated by the skills product feature and skill read or write scopes.

### Available Operations

* [Sync](#sync) - sync skills

## Sync

Synchronize the authenticated user's locally managed Claude skills with active project distributions. A 401 or 403 response means the client must remove all Gram-managed skills. Transient failures retain the last successfully managed local state.

### Example Usage

<!-- UsageSnippet language="go" operationID="syncSkills" method="post" path="/rpc/skills.sync" -->
```go
package main

import(
	"context"
	"github.com/speakeasy-api/gram/hooks/sdk"
	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
	"github.com/speakeasy-api/gram/hooks/sdk/models/operations"
	"log"
)

func main() {
    ctx := context.Background()

    s := sdk.New()

    res, err := s.Skills.Sync(ctx, operations.SyncSkillsRequest{
        XGramHookHostname: "<value>",
        Body: components.SyncSkillsRequestBody{
            Exceptions: []components.SyncSkillException{},
            Installed: []components.SyncSkillInstalled{},
            Provider: components.ProviderClaude,
        },
    }, nil)
    if err != nil {
        log.Fatal(err)
    }
    if res.SyncSkillsResult != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                                      | Type                                                                           | Required                                                                       | Description                                                                    |
| ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ |
| `ctx`                                                                          | [context.Context](https://pkg.go.dev/context#Context)                          | :heavy_check_mark:                                                             | The context to use for the request.                                            |
| `request`                                                                      | [operations.SyncSkillsRequest](../../models/operations/syncskillsrequest.md)   | :heavy_check_mark:                                                             | The request object to use for the request.                                     |
| `security`                                                                     | [operations.SyncSkillsSecurity](../../models/operations/syncskillssecurity.md) | :heavy_check_mark:                                                             | The security requirements to use for the request.                              |
| `opts`                                                                         | [][operations.Option](../../models/operations/option.md)                       | :heavy_minus_sign:                                                             | The options for this request.                                                  |

### Response

**[*operations.SyncSkillsResponse](../../models/operations/syncskillsresponse.md), error**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| apierrors.ServiceError            | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| apierrors.ServiceError            | 500, 502                          | application/json                  |
| apierrors.APIError                | 4XX, 5XX                          | \*/\*                             |