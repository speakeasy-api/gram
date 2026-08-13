package gram

import (
	"fmt"

	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/email"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/loops"
)

func loadEmailTemplateIDs(c *cli.Context) (email.TemplateIDs, error) {
	ids, err := email.ParseTemplateIDs(c.String("email-template-ids"))
	if err != nil {
		return nil, fmt.Errorf("load email template IDs: %w", err)
	}

	if loops.IsConfigured(c.String("loops-api-key")) {
		if err := ids.ValidateRegistered(); err != nil {
			return nil, fmt.Errorf("validate email template IDs: %w", err)
		}
	}

	return ids, nil
}
