package main

import (
	"context"
	"os"

	"github.com/speakeasy-api/gram/cmd"
)

func main() {
	cmd.Execute(context.Background(), os.Args)
}
