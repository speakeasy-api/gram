package main

import (
	"context"
	_ "embed"
	"os"

	"github.com/speakeasy-api/gram/infra/cmd/infra"
)

func main() {
	infra.Execute(context.Background(), os.Args)
}
