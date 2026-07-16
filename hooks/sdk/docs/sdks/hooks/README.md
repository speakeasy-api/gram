# Hooks

## Overview

Receives hook events from coding assistants for tool usage observability.

### Available Operations

* [Ingest](#ingest) - ingest hooks

## Ingest

Feature-first unified endpoint for hook events from supported coding assistants.

### Example Usage

<!-- UsageSnippet language="go" operationID="ingestHookEvent" method="post" path="/rpc/hooks.ingest" -->
```go
package main

import(
	"context"
	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
	"github.com/speakeasy-api/gram/hooks/sdk"
	"github.com/speakeasy-api/gram/hooks/sdk/models/operations"
	"log"
)

func main() {
    ctx := context.Background()

    s := sdk.New(
        sdk.WithSecurity(components.Security{
            ApikeyHeaderGramKey: "<YOUR_API_KEY_HERE>",
            ProjectSlugHeaderGramProject: "<YOUR_API_KEY_HERE>",
        }),
    )

    res, err := s.Hooks.Ingest(ctx, operations.IngestHookEventRequest{
        Body: components.IngestRequestBody{
            Event: components.HookIngestEvent{
                Type: components.TypeSkillActivated,
            },
            SchemaVersion: "<value>",
            Source: components.HookIngestSource{
                Adapter: "<value>",
            },
        },
    })
    if err != nil {
        log.Fatal(err)
    }
    if res.IngestHookResult != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                                              | Type                                                                                   | Required                                                                               | Description                                                                            |
| -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| `ctx`                                                                                  | [context.Context](https://pkg.go.dev/context#Context)                                  | :heavy_check_mark:                                                                     | The context to use for the request.                                                    |
| `request`                                                                              | [operations.IngestHookEventRequest](../../models/operations/ingesthookeventrequest.md) | :heavy_check_mark:                                                                     | The request object to use for the request.                                             |
| `opts`                                                                                 | [][operations.Option](../../models/operations/option.md)                               | :heavy_minus_sign:                                                                     | The options for this request.                                                          |

### Response

**[*operations.IngestHookEventResponse](../../models/operations/ingesthookeventresponse.md), error**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| apierrors.ServiceError            | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| apierrors.ServiceError            | 500, 502                          | application/json                  |
| apierrors.APIError                | 4XX, 5XX                          | \*/\*                             |