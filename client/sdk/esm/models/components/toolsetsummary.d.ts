import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ToolEntry } from "./toolentry.js";
/**
 * A lightweight summary of a toolset, containing only the fields needed for org-level listing (e.g. RBAC UI).
 */
export type ToolsetSummary = {
    /**
     * When the toolset was created.
     */
    createdAt: Date;
    /**
     * A short url-friendly label that uniquely identifies a resource.
     */
    defaultEnvironmentSlug?: string | undefined;
    /**
     * The ID of the toolset
     */
    id: string;
    /**
     * Whether the toolset is enabled for MCP
     */
    mcpEnabled?: boolean | undefined;
    /**
     * Whether the toolset is public in MCP
     */
    mcpIsPublic?: boolean | undefined;
    /**
     * A short url-friendly label that uniquely identifies a resource.
     */
    mcpSlug?: string | undefined;
    /**
     * The name of the toolset
     */
    name: string;
    /**
     * The organization ID this toolset belongs to
     */
    organizationId: string;
    /**
     * The project ID this toolset belongs to
     */
    projectId: string;
    /**
     * A short url-friendly label that uniquely identifies a resource.
     */
    slug: string;
    /**
     * The mode to use for tool selection
     */
    toolSelectionMode: string;
    /**
     * The tools in this toolset
     */
    tools: Array<ToolEntry>;
    /**
     * When the toolset was last updated.
     */
    updatedAt: Date;
};
/** @internal */
export declare const ToolsetSummary$inboundSchema: z.ZodMiniType<ToolsetSummary, unknown>;
export declare function toolsetSummaryFromJSON(jsonString: string): SafeParseResult<ToolsetSummary, SDKValidationError>;
//# sourceMappingURL=toolsetsummary.d.ts.map