import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * The visibility of an MCP server
 */
export declare const UpdateMcpServerFormVisibility: {
    readonly Disabled: "disabled";
    readonly Private: "private";
    readonly Public: "public";
};
/**
 * The visibility of an MCP server
 */
export type UpdateMcpServerFormVisibility = ClosedEnum<typeof UpdateMcpServerFormVisibility>;
/**
 * Form for updating an MCP server. This is a full-record replace: fields omitted from the request become null on the stored record. Exactly one of remote_mcp_server_id, tunneled_mcp_server_id, or toolset_id must be provided. Omit name to leave the existing display name unchanged; the slug is recomputed server-side from the resulting name.
 */
export type UpdateMcpServerForm = {
    /**
     * The ID of the environment to associate with the server
     */
    environmentId?: string | undefined;
    /**
     * The ID of the MCP server to update
     */
    id: string;
    /**
     * A human-readable display name for the server. Omit to leave the existing name unchanged; if provided, must be non-empty.
     */
    name?: string | undefined;
    /**
     * The ID of the remote MCP server to use as the backend
     */
    remoteMcpServerId?: string | undefined;
    /**
     * The ID of the tool variations group enabling MCP tool filtering for this server. Omit to disable filtering (cleared to null, consistent with the full-record replace semantics of the other UUID references).
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
     * The ID of the user session issuer that gates OAuth-based MCP client authentication. Omit to disable issuer-gated OAuth.
     */
    userSessionIssuerId?: string | undefined;
    /**
     * The visibility of an MCP server
     */
    visibility: UpdateMcpServerFormVisibility;
};
/** @internal */
export declare const UpdateMcpServerFormVisibility$outboundSchema: z.ZodMiniEnum<typeof UpdateMcpServerFormVisibility>;
/** @internal */
export type UpdateMcpServerForm$Outbound = {
    environment_id?: string | undefined;
    id: string;
    name?: string | undefined;
    remote_mcp_server_id?: string | undefined;
    tool_variations_group_id?: string | undefined;
    toolset_id?: string | undefined;
    tunneled_mcp_server_id?: string | undefined;
    user_session_issuer_id?: string | undefined;
    visibility: string;
};
/** @internal */
export declare const UpdateMcpServerForm$outboundSchema: z.ZodMiniType<UpdateMcpServerForm$Outbound, UpdateMcpServerForm>;
export declare function updateMcpServerFormToJSON(updateMcpServerForm: UpdateMcpServerForm): string;
//# sourceMappingURL=updatemcpserverform.d.ts.map