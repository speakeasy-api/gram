import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { TunneledMcpServer } from "./tunneledmcpserver.js";
/**
 * Result type for listing tunneled MCP servers
 */
export type ListTunneledMcpServersResult = {
  tunneledMcpServers: Array<TunneledMcpServer>;
};
/** @internal */
export declare const ListTunneledMcpServersResult$inboundSchema: z.ZodMiniType<
  ListTunneledMcpServersResult,
  unknown
>;
export declare function listTunneledMcpServersResultFromJSON(
  jsonString: string,
): SafeParseResult<ListTunneledMcpServersResult, SDKValidationError>;
//# sourceMappingURL=listtunneledmcpserversresult.d.ts.map
