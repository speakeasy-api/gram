package mcpservers

import "errors"

// Matches the disabled-only Snowflake source marker in the catalog installer.
// It is replaced by the account-specific URL before OAuth configuration.
const snowflakeSetupURL = "https://app.snowflake.com/speakeasy-mcp-setup"

var errSnowflakeSetupPending = errors.New("finish Snowflake connection setup before enabling this server")
