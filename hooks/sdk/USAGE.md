<!-- Start SDK Example Usage [usage] -->
```go
package main

import (
	"context"
	"github.com/speakeasy-api/gram/hooks/sdk"
	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
	"github.com/speakeasy-api/gram/hooks/sdk/models/operations"
	"log"
)

func main() {
	ctx := context.Background()

	s := sdk.New(
		sdk.WithSecurity(components.Security{
			ApikeyHeaderGramKey:          "<YOUR_API_KEY_HERE>",
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
<!-- End SDK Example Usage [usage] -->