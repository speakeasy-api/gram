import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { ListMcpEndpointsResult } from "../models/components/listmcpendpointsresult.js";
import { McpEndpoint } from "../models/components/mcpendpoint.js";
import { CheckMcpEndpointSlugAvailabilityRequest, CheckMcpEndpointSlugAvailabilitySecurity } from "../models/operations/checkmcpendpointslugavailability.js";
import { CreateMcpEndpointRequest, CreateMcpEndpointSecurity } from "../models/operations/createmcpendpoint.js";
import { DeleteMcpEndpointRequest, DeleteMcpEndpointSecurity } from "../models/operations/deletemcpendpoint.js";
import { GetMcpEndpointRequest, GetMcpEndpointSecurity } from "../models/operations/getmcpendpoint.js";
import { ListMcpEndpointsRequest, ListMcpEndpointsSecurity } from "../models/operations/listmcpendpoints.js";
import { UpdateMcpEndpointRequest, UpdateMcpEndpointSecurity } from "../models/operations/updatemcpendpoint.js";
export declare class McpEndpoints extends ClientSDK {
    /**
     * checkMcpEndpointSlugAvailability mcpEndpoints
     *
     * @remarks
     * Check whether an MCP endpoint slug is available. The uniqueness scope depends on whether a custom_domain_id is provided: platform-domain slugs are checked across all platform-domain endpoints (custom_domain_id IS NULL); custom-domain slugs are checked within the (custom_domain_id, slug) pair. Returns true when the slug is free.
     */
    checkSlugAvailability(request: CheckMcpEndpointSlugAvailabilityRequest, security?: CheckMcpEndpointSlugAvailabilitySecurity | undefined, options?: RequestOptions): Promise<boolean>;
    /**
     * createMcpEndpoint mcpEndpoints
     *
     * @remarks
     * Create a new MCP endpoint for an MCP server
     */
    create(request: CreateMcpEndpointRequest, security?: CreateMcpEndpointSecurity | undefined, options?: RequestOptions): Promise<McpEndpoint>;
    /**
     * deleteMcpEndpoint mcpEndpoints
     *
     * @remarks
     * Delete an MCP endpoint
     */
    delete(request: DeleteMcpEndpointRequest, security?: DeleteMcpEndpointSecurity | undefined, options?: RequestOptions): Promise<void>;
    /**
     * getMcpEndpoint mcpEndpoints
     *
     * @remarks
     * Get an MCP endpoint by id or by (custom_domain_id, slug). Provide either id, or slug with an optional custom_domain_id — not both.
     */
    get(request?: GetMcpEndpointRequest | undefined, security?: GetMcpEndpointSecurity | undefined, options?: RequestOptions): Promise<McpEndpoint>;
    /**
     * listMcpEndpoints mcpEndpoints
     *
     * @remarks
     * List MCP endpoints for a project. Optionally filter to only those associated with a specific MCP server.
     */
    list(request?: ListMcpEndpointsRequest | undefined, security?: ListMcpEndpointsSecurity | undefined, options?: RequestOptions): Promise<ListMcpEndpointsResult>;
    /**
     * updateMcpEndpoint mcpEndpoints
     *
     * @remarks
     * Update an MCP endpoint. This is a full-record replace: fields omitted from the request become null on the stored record. The id, mcp_server_id, and slug fields are required.
     */
    update(request: UpdateMcpEndpointRequest, security?: UpdateMcpEndpointSecurity | undefined, options?: RequestOptions): Promise<McpEndpoint>;
}
//# sourceMappingURL=mcpendpoints.d.ts.map