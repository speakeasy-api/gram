package feature

import "context"

// GramMCPCatalogEnabled evaluates the temporary catalog rollout using a stable
// organization ID as distinct identity and the canonical organization slug group.
// Missing organizations or providers and evaluation errors fail closed. Errors
// are preserved so callers can observe flag failures separately from catalog reads.
func GramMCPCatalogEnabled(ctx context.Context, provider Provider, organizationID, organizationSlug string) (bool, error) {
	if organizationID == "" || organizationSlug == "" || provider == nil {
		return false, nil
	}

	enabled, err := provider.IsFlagEnabled(ctx, FlagGramMCPCatalog, organizationID, map[string]string{
		"organization": organizationSlug,
	})
	return enabled && err == nil, err
}
