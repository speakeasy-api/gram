import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ExternalMCPRemote } from "./externalmcpremote.js";
/**
 * A summary of an MCP server from an external registry, returned by catalog listings
 */
export type ExternalMCPServerEntry = {
    /**
     * Description of what the server does
     */
    description: string;
    /**
     * URL to the server's icon
     */
    iconUrl?: string | undefined;
    /**
     * Whether every tool on the server is read-only
     */
    isReadOnly: boolean;
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
     * Whether the server's OAuth authorization server advertises a dynamic client registration endpoint (RFC 7591). When false, connecting requires manual setup (static OAuth client credentials or API keys).
     */
    supportsDcr: boolean;
    /**
     * Display name for the server
     */
    title?: string | undefined;
    /**
     * Number of tools the server exposes
     */
    toolCount: number;
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
export declare const ExternalMCPServerEntry$inboundSchema: z.ZodMiniType<ExternalMCPServerEntry, unknown>;
export declare function externalMCPServerEntryFromJSON(jsonString: string): SafeParseResult<ExternalMCPServerEntry, SDKValidationError>;
//# sourceMappingURL=externalmcpserverentry.d.ts.map