package stub

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

type LocalUser map[string]struct {
	UserEmail     string `json:"user_email"`
	Admin         bool   `json:"admin"`
	Organizations []struct {
		OrganizationID   string `json:"organization_id"`
		OrganizationName string `json:"organization_name"`
		OrganizationSlug string `json:"organization_slug"`
		AccountType      string `json:"account_type"`
	} `json:"organizations"`
}

var unsafeSessionData = []byte(`
{
  "1245": {
    "user_email": "user@example.com",
    "admin": false,
    "organizations": [
      {
        "organization_id": "550e8400-e29b-41d4-a716-446655440000",
        "organization_name": "Organization 123",
        "organization_slug": "organization-123",
        "account_type": "pro"
      },
      {
        "organization_id": "e0395991-d5c5-4c2f-8c3b-4eae305524ed",
        "organization_name": "Organization 456",
        "organization_slug": "organization-456",
        "account_type": "enterprise"
      }
    ]
  }
}
`)

func UnmarshalLocalUser(path string, logger *slog.Logger) (*LocalUser, error) {
	raw := unsafeSessionData

	var data LocalUser

	if path != "" {
		file, err := os.Open(filepath.Clean(path))
		if err != nil {
			logger.WarnContext(context.Background(), "failed to open local env file, defaulting to inlined data (localdev.go)", attr.SlogError(err))
		} else if file != nil {
			defer o11y.LogDefer(context.Background(), logger, func() error {
				return file.Close()
			})

			raw, err = io.ReadAll(file)
			if err != nil {
				return nil, fmt.Errorf("failed to read local env file: %w", err)
			}
		}
	}

	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal local env file: %w", err)
	}

	return &data, nil
}
