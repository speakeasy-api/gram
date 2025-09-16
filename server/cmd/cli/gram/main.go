package main

import (
	"log"
	"os"

	"github.com/speakeasy-api/gram/server/cmd/cli/gram/app"
)

func main() {
	if err := app.NewCLI().Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
