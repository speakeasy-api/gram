// Command authzcases writes the cases the dashboard's tests replay against its
// own matcher and tool classifier, answered by the server. Run it from the server
// directory after authzgen, or through `mise run gen:authz`, which runs both.
package main

import (
	"fmt"
	"os"

	"github.com/speakeasy-api/gram/server/internal/authz/authzcases"
)

func main() {
	files := []struct {
		path   string
		render func() ([]byte, error)
	}{
		{authzcases.Path, authzcases.File},
		{authzcases.DispositionPath, authzcases.DispositionFile},
	}
	for _, file := range files {
		content, err := file.render()
		if err != nil {
			fmt.Fprintf(os.Stderr, "authzcases: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(file.path, content, 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "authzcases: %v\n", err)
			os.Exit(1)
		}
	}
}
