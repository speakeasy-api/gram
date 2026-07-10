import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * An MCP endpoint registered under a custom domain, with its parent MCP server and project denormalised for display in the dashboard's delete-impact preview.
 */
export type CustomDomainMcpEndpoint = {
    /**
     * The ID of the MCP endpoint
     */
    id: string;
    /**
     * The ID of the parent MCP server
     */
    mcpServerId: string;
    /**
     * The display name of the parent MCP server. May be empty if the parent has no configured name.
     */
    mcpServerName?: string | undefined;
    /**
     * The url-friendly slug of the parent MCP server. May be empty if the parent has no configured slug.
     */
    mcpServerSlug?: string | undefined;
    /**
     * The ID of the project the endpoint belongs to
     */
    projectId: string;
    /**
     * The display name of the project the endpoint belongs to
     */
    projectName: string;
    /**
     * The url-friendly slug of the project the endpoint belongs to
     */
    projectSlug: string;
    /**
     * The endpoint slug
     */
    slug: string;
};
/** @internal */
export declare const CustomDomainMcpEndpoint$inboundSchema: z.ZodMiniType<CustomDomainMcpEndpoint, unknown>;
export declare function customDomainMcpEndpointFromJSON(jsonString: string): SafeParseResult<CustomDomainMcpEndpoint, SDKValidationError>;
//# sourceMappingURL=customdomainmcpendpoint.d.ts.map