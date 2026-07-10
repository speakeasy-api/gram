import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * The visibility of an MCP server
 */
export declare const CreateMcpServerFormVisibility: {
    readonly Disabled: "disabled";
    readonly Private: "private";
    readonly Public: "public";
};
/**
 * The visibility of an MCP server
 */
export type CreateMcpServerFormVisibility = ClosedEnum<typeof CreateMcpServerFormVisibility>;
/**
 * Form for creating a new MCP server. Exactly one of remote_mcp_server_id, tunneled_mcp_server_id, or toolset_id must be provided.
 */
export type CreateMcpServerForm = {
    /**
     * The ID of the environment to associate with the server
     */
    environmentId?: string | undefined;
    /**
     * A human-readable display name for the server
     */
    name: string;
    /**
     * The ID of the remote MCP server to use as the backend
     */
    remoteMcpServerId?: string | undefined;
    /**
     * The ID of the tool variations group enabling MCP tool filtering for this server. Omit to leave filtering disabled.
     */
    toolVariationsGroupId?: string | undefined;
    /**
     * The ID of the toolset to use as the backend
     */
    toolsetId?: string | undefined;
    /**
     * The ID of the tunneled MCP server to use as the backend
     */
    tunneledMcpServerId?: string | undefined;
    /**
     * The ID of the user session issuer that gates OAuth-based MCP client authentication. When set, MCP clients are required to authenticate against this issuer before connecting.
     */
    userSessionIssuerId?: string | undefined;
    /**
     * The visibility of an MCP server
     */
    visibility: CreateMcpServerFormVisibility;
};
/** @internal */
export declare const CreateMcpServerFormVisibility$outboundSchema: z.ZodMiniEnum<typeof CreateMcpServerFormVisibility>;
/** @internal */
export type CreateMcpServerForm$Outbound = {
    environment_id?: string | undefined;
    name: string;
    remote_mcp_server_id?: string | undefined;
    tool_variations_group_id?: string | undefined;
    toolset_id?: string | undefined;
    tunneled_mcp_server_id?: string | undefined;
    user_session_issuer_id?: string | undefined;
    visibility: string;
};
/** @internal */
export declare const CreateMcpServerForm$outboundSchema: z.ZodMiniType<CreateMcpServerForm$Outbound, CreateMcpServerForm>;
export declare function createMcpServerFormToJSON(createMcpServerForm: CreateMcpServerForm): string;
//# sourceMappingURL=createmcpserverform.d.ts.map