import { createMCPClient, MCPClientError } from "@ai-sdk/mcp";

type MCPClientConfig = Parameters<typeof createMCPClient>[0];
type MCPClient = Awaited<ReturnType<typeof createMCPClient>>;

/**
 * JSON-RPC error code an MCP server returns when it rejects the protocol
 * version the client asked for.
 */
const UNSUPPORTED_PROTOCOL_VERSION_CODE = -32022;

/**
 * Creates an `@ai-sdk/mcp` client, falling back to the legacy `initialize`
 * handshake when the server rejects the client's newest protocol version.
 *
 * By default the client first sends `server/discover` at its newest protocol
 * version. A server that only implements older revisions may answer with
 * -32022 (unsupported protocol version), which the client rethrows instead of
 * falling back, so the connection fails even though the server would accept
 * an older version. Retrying once with discovery disabled negotiates through
 * `initialize` instead, while servers that accept the newest version keep
 * using it.
 */
export async function createMCPClientWithVersionFallback(
  config: MCPClientConfig,
): Promise<MCPClient> {
  try {
    return await createMCPClient(config);
  } catch (error) {
    if (
      config.protocolVersionDiscovery === false ||
      !isUnsupportedProtocolVersionError(error)
    ) {
      throw error;
    }
    return createMCPClient({ ...config, protocolVersionDiscovery: false });
  }
}

function isUnsupportedProtocolVersionError(error: unknown): boolean {
  return (
    MCPClientError.isInstance(error) &&
    error.code === UNSUPPORTED_PROTOCOL_VERSION_CODE
  );
}
