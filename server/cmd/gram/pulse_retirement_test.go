package gram

import (
	"github.com/urfave/cli/v2"
	"testing"
)

func TestServiceCommandsDoNotRequirePulseCredentials(t *testing.T) {
	for _, command := range []*cli.Command{newStartCommand(), newWorkerCommand()} {
		for _, f := range command.Flags {
			for _, name := range f.Names() {
				if name == "pulse-registry-tenant" || name == "pulse-registry-api-key" {
					t.Errorf("%s still registers retired credential flag %s", command.Name, name)
				}
			}
		}
	}
}
