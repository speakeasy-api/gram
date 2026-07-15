import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Top MCP server by tool call count
 */
export type TopServer = {
  /**
   * MCP server name
   */
  serverName: string;
  /**
   * Total number of tool calls
   */
  toolCallCount: number;
};
/** @internal */
export declare const TopServer$inboundSchema: z.ZodMiniType<TopServer, unknown>;
export declare function topServerFromJSON(
  jsonString: string,
): SafeParseResult<TopServer, SDKValidationError>;
//# sourceMappingURL=topserver.d.ts.map
