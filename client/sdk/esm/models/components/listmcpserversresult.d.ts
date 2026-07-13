import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { McpServer } from "./mcpserver.js";
/**
 * Result type for listing MCP servers
 */
export type ListMcpServersResult = {
  mcpServers: Array<McpServer>;
};
/** @internal */
export declare const ListMcpServersResult$inboundSchema: z.ZodMiniType<
  ListMcpServersResult,
  unknown
>;
export declare function listMcpServersResultFromJSON(
  jsonString: string,
): SafeParseResult<ListMcpServersResult, SDKValidationError>;
//# sourceMappingURL=listmcpserversresult.d.ts.map
