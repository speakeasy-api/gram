package admin

import "context"

type supportHandoffIssuer interface {
	Issue(ctx context.Context, organizationID string) (string, error)
}
