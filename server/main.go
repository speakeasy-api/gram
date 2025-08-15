package main

import (
	"context"
	_ "embed"
	"os"

	"github.com/speakeasy-api/gram/server/cmd/gram"
	"github.com/speakeasy-api/gram/server/internal/about"
)

// We are embedding the OpenAPI document here because this file sits above the
// gen/http directory. The go:embed directive does not all '../' to walk up the
// directory tree, so we need to embed from above the desired directory.
// This then means we need to provide a way to drill down the contents to other
// packages under this module which is why `about.SetOpenAPIDoc` exists.
//
//go:embed gen/http/openapi3.yaml
var openapi []byte

func main() {
	about.SetOpenAPIDoc(openapi)
	gram.Execute(context.Background(), os.Args)
}
