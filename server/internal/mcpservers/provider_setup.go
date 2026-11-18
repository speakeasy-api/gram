package mcpservers

import "errors"

// Matches the disabled-only account-provider source markers in the catalog installer.
// It is replaced by the account-specific URL before OAuth configuration.
const snowflakeSetupURL = "https://app.snowflake.com/speakeasy-mcp-setup"

const oktaSetupURL = "https://www.okta.com/speakeasy-mcp-setup"

var errProviderSetupPending = errors.New("finish connection setup before enabling this server")

func isPendingProviderSetupURL(value string) bool {
	return value == snowflakeSetupURL || value == oktaSetupURL
}
