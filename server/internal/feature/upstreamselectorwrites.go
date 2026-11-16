package feature

import "context"

// UpstreamSelectorWrites evaluates FlagAuthClientUpstreamSelectorWrites for
// an organization from locally evaluated flags. A missing provider or
// organization reports false; callers must treat an error as false too.
func UpstreamSelectorWrites(flags Provider) func(ctx context.Context, organizationID string) (bool, error) {
	return func(ctx context.Context, organizationID string) (bool, error) {
		if flags == nil || organizationID == "" {
			return false, nil
		}
		return flags.IsFlagEnabledLocal(ctx, FlagAuthClientUpstreamSelectorWrites, organizationID, nil, nil)
	}
}
