package shared

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/internal/plugins/installmode"
)

// InstallModeEnum restricts an attribute to the plugin install modes the
// server validates in installmode.Parse.
func InstallModeEnum() {
	values := installmode.Values()
	enum := make([]any, 0, len(values))
	for _, mode := range values {
		enum = append(enum, string(mode))
	}
	Enum(enum...)
}
