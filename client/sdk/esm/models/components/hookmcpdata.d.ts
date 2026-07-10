import * as z from "zod/v4-mini";
/**
 * MCP feature payload.
 */
export type HookMCPData = {
  /**
   * MCP server command, when available.
   */
  command?: string | undefined;
  /**
   * JSON-encoded MCP tool result, when reported as a string.
   */
  resultJson?: string | undefined;
  /**
   * Stable server identity inferred by the hook adapter.
   */
  serverIdentity?: string | undefined;
  /**
   * Provider-reported MCP server name.
   */
  serverName?: string | undefined;
  /**
   * MCP server URL, when available.
   */
  url?: string | undefined;
};
/** @internal */
export type HookMCPData$Outbound = {
  command?: string | undefined;
  result_json?: string | undefined;
  server_identity?: string | undefined;
  server_name?: string | undefined;
  url?: string | undefined;
};
/** @internal */
export declare const HookMCPData$outboundSchema: z.ZodMiniType<
  HookMCPData$Outbound,
  HookMCPData
>;
export declare function hookMCPDataToJSON(hookMCPData: HookMCPData): string;
//# sourceMappingURL=hookmcpdata.d.ts.map
