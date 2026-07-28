import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Whether this server is required or optional.
 */
export declare const PluginServerPolicy: {
  readonly Required: "required";
  readonly Optional: "optional";
};
/**
 * Whether this server is required or optional.
 */
export type PluginServerPolicy = ClosedEnum<typeof PluginServerPolicy>;
export type PluginServer = {
  createdAt: Date;
  /**
   * Display name shown in generated plugin config.
   */
  displayName: string;
  /**
   * Unique plugin server identifier.
   */
  id: string;
  /**
   * Gram MCP server ID. Set when this server is Remote MCP-backed (exactly one of toolset_id / mcp_server_id is set).
   */
  mcpServerId?: string | undefined;
  /**
   * Whether this server is required or optional.
   */
  policy: PluginServerPolicy;
  /**
   * Ordering within the plugin.
   */
  sortOrder: number;
  /**
   * Gram toolset ID. Set when this server is toolset-backed (exactly one of toolset_id / mcp_server_id is set).
   */
  toolsetId?: string | undefined;
};
/** @internal */
export declare const PluginServerPolicy$inboundSchema: z.ZodMiniEnum<
  typeof PluginServerPolicy
>;
/** @internal */
export declare const PluginServer$inboundSchema: z.ZodMiniType<
  PluginServer,
  unknown
>;
export declare function pluginServerFromJSON(
  jsonString: string,
): SafeParseResult<PluginServer, SDKValidationError>;
//# sourceMappingURL=pluginserver.d.ts.map
