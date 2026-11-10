package feature

import "context"

// OrgEncryptedIntegrationCredentials reports whether
// FlagOrgEncryptedIntegrationCredentials is on for an organization. Any
// evaluation failure keeps it off.
func OrgEncryptedIntegrationCredentials(ctx context.Context, flags Provider, organizationID string) bool {
	if flags == nil || organizationID == "" {
		return false
	}
	enabled, err := flags.IsFlagEnabledLocal(ctx, FlagOrgEncryptedIntegrationCredentials, organizationID, nil, nil)
	return err == nil && enabled
}
