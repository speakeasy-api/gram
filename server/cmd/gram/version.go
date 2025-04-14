package gram

import (
	"fmt"

	"github.com/urfave/cli/v2"
)

func newVersionCommand() *cli.Command {
	return &cli.Command{
		Name:  "version",
		Usage: "Print the version of the Gram API server",
		Action: func(c *cli.Context) error {
			_, err := fmt.Println(GitSHA)
			return err
		},
	}
}
