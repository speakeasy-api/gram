// Command authzgen writes the files generated from the authorization model.
// Run it from the server directory, or through `mise run gen:authz`.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authz/authzgen"
)

func main() {
	files, err := authzgen.Render(authz.Model)
	if err != nil {
		fmt.Fprintf(os.Stderr, "authzgen: %v\n", err)
		os.Exit(1)
	}
	for _, file := range files {
		if err := os.MkdirAll(filepath.Dir(file.Path), 0o750); err != nil {
			fmt.Fprintf(os.Stderr, "authzgen: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(file.Path, file.Content, 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "authzgen: %v\n", err)
			os.Exit(1)
		}
	}
}
