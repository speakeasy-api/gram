import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { RemoteMcpServerHeader } from "./remotemcpserverheader.js";
/**
 * A remote MCP server configuration
 */
export type RemoteMcpServer = {
    /**
     * When the remote MCP server was created
     */
    createdAt: Date;
    /**
     * Headers configured for this remote MCP server
     */
    headers: Array<RemoteMcpServerHeader>;
    /**
     * The ID of the remote MCP server
     */
    id: string;
    /**
     * Optional human-readable name for the remote MCP server
     */
    name?: string | undefined;
    /**
     * The project ID this remote MCP server belongs to
     */
    projectId: string;
    /**
     * URL-friendly slug derived from the URL and ID.
     */
    slug?: string | undefined;
    /**
     * The transport type for the remote MCP server
     */
    transportType: string;
    /**
     * When the remote MCP server was last updated
     */
    updatedAt: Date;
    /**
     * The URL of the remote MCP server
     */
    url: string;
};
/** @internal */
export declare const RemoteMcpServer$inboundSchema: z.ZodMiniType<RemoteMcpServer, unknown>;
export declare function remoteMcpServerFromJSON(jsonString: string): SafeParseResult<RemoteMcpServer, SDKValidationError>;
//# sourceMappingURL=remotemcpserver.d.ts.map