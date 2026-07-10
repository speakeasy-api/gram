import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ExternalMCPRemote } from "./externalmcpremote.js";
import { ExternalMCPTool } from "./externalmcptool.js";
/**
 * An MCP server from an external registry
 */
export type ExternalMCPServer = {
    /**
     * Description of what the server does
     */
    description: string;
    /**
     * URL to the server's icon
     */
    iconUrl?: string | undefined;
    /**
     * ID of the attached MCP server when this server is listed from a Collection (mcp_server-backed attachment)
     */
    mcpServerId?: string | undefined;
    /**
     * Opaque metadata from the registry
     */
    meta?: any | undefined;
    /**
     * ID of the internal collection registry this server came from
     */
    organizationMcpCollectionRegistryId?: string | undefined;
    /**
     * ID of the external MCP registry this server came from
     */
    registryId?: string | undefined;
    /**
     * Server specifier used to look up in the registry (e.g., 'io.github.user/server')
     */
    registrySpecifier: string;
    /**
     * Available remote endpoints for the server
     */
    remotes?: Array<ExternalMCPRemote> | undefined;
    /**
     * Display name for the server
     */
    title?: string | undefined;
    /**
     * Tools available on the server
     */
    tools?: Array<ExternalMCPTool> | undefined;
    /**
     * ID of the attached toolset when this server is listed from a Collection (toolset-backed attachment)
     */
    toolsetId?: string | undefined;
    /**
     * Semantic version of the server
     */
    version: string;
};
/** @internal */
export declare const ExternalMCPServer$inboundSchema: z.ZodMiniType<ExternalMCPServer, unknown>;
export declare function externalMCPServerFromJSON(jsonString: string): SafeParseResult<ExternalMCPServer, SDKValidationError>;
//# sourceMappingURL=externalmcpserver.d.ts.map