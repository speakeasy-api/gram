package feature

import "context"

// UpstreamIssuerFromAssociations reports whether the organization takes its
// MCP servers' upstream issuer from their client associations. Any
// evaluation failure keeps the column.
func UpstreamIssuerFromAssociations(ctx context.Context, flags Provider, organizationID string) bool {
	if flags == nil || organizationID == "" {
		return false
	}
	enabled, err := flags.IsFlagEnabledLocal(ctx, FlagUpstreamIssuerFromAssociations, organizationID, nil, nil)
	return err == nil && enabled
}
