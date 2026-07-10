import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { TunneledMcpConnection } from "./tunneledmcpconnection.js";
/**
 * Live connection details for a tunneled MCP server
 */
export type TunneledMcpServerConnections = {
    /**
     * Number of active tunnel connections currently visible in Redis
     */
    activeConnectionCount: number;
    /**
     * Total MCP consumer sessions currently pinned across active tunnel connections
     */
    activeConsumerSessionCount: number;
    /**
     * Live tunnel connections currently visible in Redis
     */
    connections: Array<TunneledMcpConnection>;
};
/** @internal */
export declare const TunneledMcpServerConnections$inboundSchema: z.ZodMiniType<TunneledMcpServerConnections, unknown>;
export declare function tunneledMcpServerConnectionsFromJSON(jsonString: string): SafeParseResult<TunneledMcpServerConnections, SDKValidationError>;
//# sourceMappingURL=tunneledmcpserverconnections.d.ts.map