package templates

import (
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/internal/jsonschema"
)

func validateInputSchema(rawInput io.Reader) error {
	if err := jsonschema.ValidateInputSchema(rawInput); err != nil {
		return fmt.Errorf("validate input schema: %w", err)
	}
	return nil
}
