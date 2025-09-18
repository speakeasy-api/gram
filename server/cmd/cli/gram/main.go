package main

import (
	"os"

	"github.com/speakeasy-api/gram/server/cmd/cli/gram/app"
	"github.com/speakeasy-api/gram/server/cmd/cli/gram/log"
)

func main() {
	if err := app.NewCLI().Run(os.Args); err != nil {
		log.L.Error("CLI failed", "error", err)
		os.Exit(1)
	}
}
