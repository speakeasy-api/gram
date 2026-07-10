import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Derived live connection status for a tunneled MCP server source
 */
export declare const ConnectionStatus: {
    readonly Connected: "connected";
    readonly Inactive: "inactive";
    readonly NeverConnected: "never_connected";
};
/**
 * Derived live connection status for a tunneled MCP server source
 */
export type ConnectionStatus = ClosedEnum<typeof ConnectionStatus>;
/**
 * Stored lifecycle status for a tunneled MCP server source
 */
export declare const TunneledMcpServerStatus: {
    readonly Created: "created";
    readonly Active: "active";
    readonly Revoked: "revoked";
};
/**
 * Stored lifecycle status for a tunneled MCP server source
 */
export type TunneledMcpServerStatus = ClosedEnum<typeof TunneledMcpServerStatus>;
/**
 * A customer-hosted MCP server connected through a tunnel
 */
export type TunneledMcpServer = {
    /**
     * Number of active tunnel connections currently visible in Redis
     */
    activeConnectionCount: number;
    /**
     * Total MCP consumer sessions currently pinned across active tunnel connections
     */
    activeConsumerSessionCount: number;
    /**
     * Most recent agent version reported by the tunnel
     */
    agentVersion?: string | undefined;
    /**
     * Derived live connection status for a tunneled MCP server source
     */
    connectionStatus: ConnectionStatus;
    /**
     * When the tunneled MCP server source was created
     */
    createdAt: Date;
    /**
     * The ID of the tunneled MCP server
     */
    id: string;
    /**
     * Non-secret prefix of the tunnel key
     */
    keyPrefix: string;
    /**
     * Most recent persisted heartbeat timestamp
     */
    lastSeenAt?: Date | undefined;
    /**
     * Human-readable name for the tunneled MCP server
     */
    name: string;
    /**
     * The project ID this tunneled MCP server belongs to
     */
    projectId: string;
    /**
     * Stored lifecycle status for a tunneled MCP server source
     */
    status: TunneledMcpServerStatus;
    /**
     * When the tunneled MCP server source was last updated
     */
    updatedAt: Date;
};
/** @internal */
export declare const ConnectionStatus$inboundSchema: z.ZodMiniEnum<typeof ConnectionStatus>;
/** @internal */
export declare const TunneledMcpServerStatus$inboundSchema: z.ZodMiniEnum<typeof TunneledMcpServerStatus>;
/** @internal */
export declare const TunneledMcpServer$inboundSchema: z.ZodMiniType<TunneledMcpServer, unknown>;
export declare function tunneledMcpServerFromJSON(jsonString: string): SafeParseResult<TunneledMcpServer, SDKValidationError>;
//# sourceMappingURL=tunneledmcpserver.d.ts.map