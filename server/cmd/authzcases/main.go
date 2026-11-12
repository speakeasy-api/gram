// Command authzcases writes the cases the dashboard's tests replay against its
// own matcher, answered by the server's evaluator. Run it from the server
// directory after authzgen, or through `mise run gen:authz`, which runs both.
package main

import (
	"fmt"
	"os"

	"github.com/speakeasy-api/gram/server/internal/authz/authzcases"
)

func main() {
	content, err := authzcases.File()
	if err != nil {
		fmt.Fprintf(os.Stderr, "authzcases: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(authzcases.Path, content, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "authzcases: %v\n", err)
		os.Exit(1)
	}
}
