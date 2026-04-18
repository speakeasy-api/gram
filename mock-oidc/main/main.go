package main

import (
	"context"
	"fmt"
	"os"

	mockoidc "github.com/speakeasy-api/gram/mock-oidc"
)

func main() {
	app := mockoidc.NewApp()
	if err := app.RunContext(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
