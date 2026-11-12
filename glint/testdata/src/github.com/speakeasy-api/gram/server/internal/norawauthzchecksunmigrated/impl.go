package norawauthzchecksunmigrated

import "github.com/speakeasy-api/gram/server/internal/authz"

func literal(project string) authz.Check {
	return authz.Check{Scope: "project:read", ResourceID: project}
}
