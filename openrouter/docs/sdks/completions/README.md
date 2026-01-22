# Completions

## Overview

### Available Operations

* [Generate](#generate) - Create a completion

## Generate

Creates a completion for the provided prompt and parameters. Supports both streaming and non-streaming modes.

### Example Usage

<!-- UsageSnippet language="go" operationID="createCompletions" method="post" path="/completions" -->
```go
package main

import(
	"context"
	"github.com/speakeasy-api/gram/openrouter"
	"github.com/speakeasy-api/gram/openrouter/models/components"
	"log"
)

func main() {
    ctx := context.Background()

    s := openrouter.New(
        openrouter.WithSecurity("<YOUR_BEARER_TOKEN_HERE>"),
    )

    res, err := s.Completions.Generate(ctx, components.CompletionCreateParams{
        Prompt: components.CreatePromptArrayOfStr(
            []string{},
        ),
    })
    if err != nil {
        log.Fatal(err)
    }
    if res.CompletionResponse != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                                              | Type                                                                                   | Required                                                                               | Description                                                                            |
| -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| `ctx`                                                                                  | [context.Context](https://pkg.go.dev/context#Context)                                  | :heavy_check_mark:                                                                     | The context to use for the request.                                                    |
| `request`                                                                              | [components.CompletionCreateParams](../../models/components/completioncreateparams.md) | :heavy_check_mark:                                                                     | The request object to use for the request.                                             |
| `opts`                                                                                 | [][operations.Option](../../models/operations/option.md)                               | :heavy_minus_sign:                                                                     | The options for this request.                                                          |

### Response

**[*operations.CreateCompletionsResponse](../../models/operations/createcompletionsresponse.md), error**

### Errors

| Error Type          | Status Code         | Content Type        |
| ------------------- | ------------------- | ------------------- |
| apierrors.ChatError | 400, 401, 429       | application/json    |
| apierrors.ChatError | 500                 | application/json    |
| apierrors.APIError  | 4XX, 5XX            | \*/\*               |