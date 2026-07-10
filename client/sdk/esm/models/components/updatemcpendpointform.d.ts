import * as z from "zod/v4-mini";
/**
 * Form for updating an MCP endpoint. This is a full-record replace: the custom_domain_id field omitted from the request becomes null on the stored record. Platform-domain endpoint slugs (no custom_domain_id) must be prefixed with the organization slug.
 */
export type UpdateMcpEndpointForm = {
    /**
     * The ID of the custom domain to register the endpoint slug under. Omit to move the endpoint to a platform domain.
     */
    customDomainId?: string | undefined;
    /**
     * The ID of the MCP endpoint to update
     */
    id: string;
    /**
     * The ID of the MCP server this endpoint addresses
     */
    mcpServerId: string;
    /**
     * A url-friendly label (up to 128 characters) that addresses an MCP server through a slug-based URL. Platform-domain slugs (no custom domain) must be prefixed with the organization slug.
     */
    slug: string;
};
/** @internal */
export type UpdateMcpEndpointForm$Outbound = {
    custom_domain_id?: string | undefined;
    id: string;
    mcp_server_id: string;
    slug: string;
};
/** @internal */
export declare const UpdateMcpEndpointForm$outboundSchema: z.ZodMiniType<UpdateMcpEndpointForm$Outbound, UpdateMcpEndpointForm>;
export declare function updateMcpEndpointFormToJSON(updateMcpEndpointForm: UpdateMcpEndpointForm): string;
//# sourceMappingURL=updatemcpendpointform.d.ts.map