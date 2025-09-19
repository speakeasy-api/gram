package main

import (
	"os"

	"github.com/speakeasy-api/gram/cli/internal/app"
	"github.com/speakeasy-api/gram/cli/internal/log"
)

func main() {
	if err := app.NewCLI().Run(os.Args); err != nil {
		log.L.Error("CLI failed", "error", err)
		os.Exit(1)
	}
}
