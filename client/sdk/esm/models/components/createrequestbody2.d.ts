import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * Visibility of the collection
 */
export declare const CreateRequestBody2Visibility: {
    readonly Public: "public";
    readonly Private: "private";
};
/**
 * Visibility of the collection
 */
export type CreateRequestBody2Visibility = ClosedEnum<typeof CreateRequestBody2Visibility>;
export type CreateRequestBody2 = {
    /**
     * Description of the collection
     */
    description?: string | undefined;
    /**
     * Registry namespace (e.g., 'com.speakeasy.acme.my-tools')
     */
    mcpRegistryNamespace: string;
    /**
     * MCP server IDs to attach to the collection
     */
    mcpServerIds?: Array<string> | undefined;
    /**
     * Display name for the collection
     */
    name: string;
    /**
     * URL-friendly identifier for the collection
     */
    slug: string;
    /**
     * Toolset IDs to attach to the collection
     */
    toolsetIds?: Array<string> | undefined;
    /**
     * Visibility of the collection
     */
    visibility?: CreateRequestBody2Visibility | undefined;
};
/** @internal */
export declare const CreateRequestBody2Visibility$outboundSchema: z.ZodMiniEnum<typeof CreateRequestBody2Visibility>;
/** @internal */
export type CreateRequestBody2$Outbound = {
    description?: string | undefined;
    mcp_registry_namespace: string;
    mcp_server_ids?: Array<string> | undefined;
    name: string;
    slug: string;
    toolset_ids?: Array<string> | undefined;
    visibility: string;
};
/** @internal */
export declare const CreateRequestBody2$outboundSchema: z.ZodMiniType<CreateRequestBody2$Outbound, CreateRequestBody2>;
export declare function createRequestBody2ToJSON(createRequestBody2: CreateRequestBody2): string;
//# sourceMappingURL=createrequestbody2.d.ts.map