import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type TunneledMcpConnection = {
    /**
     * Number of MCP consumer sessions currently pinned to this tunnel connection
     */
    activeConsumerSessions: number;
    /**
     * Number of active request substreams on this tunnel session
     */
    activeSubstreams: number;
    /**
     * Tunnel agent version reported by the connection
     */
    agentVersion?: string | undefined;
    /**
     * When this tunnel session connected
     */
    connectedAt: Date;
    /**
     * Gateway session ID for a live tunnel connection
     */
    gatewaySessionId: string;
    /**
     * Most recent heartbeat observed for this tunnel session
     */
    lastHeartbeatAt: Date;
    /**
     * User-provided tunnel metadata reported by the agent
     */
    metadata: {
        [k: string]: string;
    };
    /**
     * Remote address reported by the gateway
     */
    remoteAddr?: string | undefined;
    /**
     * Customer-declared version of the MCP service behind this tunnel connection
     */
    serviceVersion: string;
};
/** @internal */
export declare const TunneledMcpConnection$inboundSchema: z.ZodMiniType<TunneledMcpConnection, unknown>;
export declare function tunneledMcpConnectionFromJSON(jsonString: string): SafeParseResult<TunneledMcpConnection, SDKValidationError>;
//# sourceMappingURL=tunneledmcpconnection.d.ts.map