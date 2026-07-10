import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { CanonicalToolAttributes } from "./canonicaltoolattributes.js";
import { ToolAnnotations } from "./toolannotations.js";
import { ToolVariation } from "./toolvariation.js";
/**
 * A platform-owned tool served directly by the platform
 */
export type PlatformToolDefinition = {
    /**
     * Tool annotations providing behavioral hints about the tool
     */
    annotations?: ToolAnnotations | undefined;
    /**
     * The original details of a tool
     */
    canonical?: CanonicalToolAttributes | undefined;
    /**
     * The canonical name of the tool. Will be the same as the name if there is no variation.
     */
    canonicalName: string;
    /**
     * Confirmation mode for the tool
     */
    confirm?: string | undefined;
    /**
     * Prompt for the confirmation
     */
    confirmPrompt?: string | undefined;
    /**
     * The creation date of the tool.
     */
    createdAt: Date;
    /**
     * Description of the tool
     */
    description: string;
    /**
     * The ID of the tool
     */
    id: string;
    /**
     * The name of the tool
     */
    name: string;
    /**
     * Optional owning entity ID
     */
    ownerId?: string | undefined;
    /**
     * The entity kind that owns this tool's lifecycle
     */
    ownerKind?: string | undefined;
    /**
     * The ID of the project
     */
    projectId: string;
    /**
     * JSON schema for the request
     */
    schema: string;
    /**
     * Version of the schema
     */
    schemaVersion?: string | undefined;
    /**
     * The backing platform tool source (for example: logs)
     */
    sourceSlug: string;
    /**
     * Summarizer for the tool
     */
    summarizer?: string | undefined;
    /**
     * The URN of this tool
     */
    toolUrn: string;
    /**
     * The last update date of the tool.
     */
    updatedAt: Date;
    variation?: ToolVariation | undefined;
};
/** @internal */
export declare const PlatformToolDefinition$inboundSchema: z.ZodMiniType<PlatformToolDefinition, unknown>;
export declare function platformToolDefinitionFromJSON(jsonString: string): SafeParseResult<PlatformToolDefinition, SDKValidationError>;
//# sourceMappingURL=platformtooldefinition.d.ts.map