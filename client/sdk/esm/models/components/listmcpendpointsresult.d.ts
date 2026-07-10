import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { McpEndpoint } from "./mcpendpoint.js";
/**
 * Result type for listing MCP endpoints
 */
export type ListMcpEndpointsResult = {
    mcpEndpoints: Array<McpEndpoint>;
};
/** @internal */
export declare const ListMcpEndpointsResult$inboundSchema: z.ZodMiniType<ListMcpEndpointsResult, unknown>;
export declare function listMcpEndpointsResultFromJSON(jsonString: string): SafeParseResult<ListMcpEndpointsResult, SDKValidationError>;
//# sourceMappingURL=listmcpendpointsresult.d.ts.map