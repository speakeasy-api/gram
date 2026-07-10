import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { RemoteMcpServer } from "./remotemcpserver.js";
/**
 * Result type for listing remote MCP servers
 */
export type ListServersResult = {
    remoteMcpServers: Array<RemoteMcpServer>;
};
/** @internal */
export declare const ListServersResult$inboundSchema: z.ZodMiniType<ListServersResult, unknown>;
export declare function listServersResultFromJSON(jsonString: string): SafeParseResult<ListServersResult, SDKValidationError>;
//# sourceMappingURL=listserversresult.d.ts.map