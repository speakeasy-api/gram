package main

import (
	"context"
	"github.com/speakeasy-api/gram/cmd/gram"
	"os"
)

func main() {
	gram.Execute(context.Background(), os.Args)
}
