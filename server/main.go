package main

import (
	"context"
	"os"

	"github.com/speakeasy-api/gram/cmd/gram"
)

func main() {
	gram.Execute(context.Background(), os.Args)
}
