<!-- Start SDK Example Usage [usage] -->
```go
package main

import (
	"context"
	"github.com/speakeasy-api/gram/responses"
	"github.com/speakeasy-api/gram/responses/models/components"
	"github.com/speakeasy-api/gram/responses/optionalnullable"
	"log"
)

func main() {
	ctx := context.Background()

	s := responses.New()

	res, err := s.Create(ctx, &components.CreateResponseBody{
		Input: optionalnullable.From(responses.Pointer(components.CreateInputArrayOfItemParam(
			[]components.ItemParam{},
		))),
		PreviousResponseID: optionalnullable.From(responses.Pointer("resp_123")),
	})
	if err != nil {
		log.Fatal(err)
	}
	if res.ResponseResource != nil {
		defer res.Object.Close()

		for res.Object.Next() {
			event := res.Object.Value()
			log.Print(event)
			// Handle the event
		}
	}
}

```
<!-- End SDK Example Usage [usage] -->