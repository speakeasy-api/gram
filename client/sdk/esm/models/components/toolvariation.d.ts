import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type ToolVariation = {
    /**
     * The confirmation mode for the tool variation
     */
    confirm?: string | undefined;
    /**
     * The confirmation prompt for the tool variation
     */
    confirmPrompt?: string | undefined;
    /**
     * The creation date of the tool variation
     */
    createdAt: string;
    /**
     * The description of the tool variation
     */
    description?: string | undefined;
    /**
     * Override: if true, the tool may perform destructive updates
     */
    destructiveHint?: boolean | undefined;
    /**
     * The ID of the tool variation group
     */
    groupId: string;
    /**
     * The ID of the tool variation
     */
    id: string;
    /**
     * Override: if true, repeated calls have no additional effect
     */
    idempotentHint?: boolean | undefined;
    /**
     * The name of the tool variation
     */
    name?: string | undefined;
    /**
     * Override: if true, the tool interacts with external entities
     */
    openWorldHint?: boolean | undefined;
    /**
     * Override: if true, the tool does not modify its environment
     */
    readOnlyHint?: boolean | undefined;
    /**
     * The name of the source tool
     */
    srcToolName: string;
    /**
     * The URN of the source tool
     */
    srcToolUrn: string;
    /**
     * The summarizer of the tool variation
     */
    summarizer?: string | undefined;
    /**
     * The tags of the tool variation
     */
    tags?: Array<string> | undefined;
    /**
     * Display name override for the tool
     */
    title?: string | undefined;
    /**
     * The last update date of the tool variation
     */
    updatedAt: string;
};
/** @internal */
export declare const ToolVariation$inboundSchema: z.ZodMiniType<ToolVariation, unknown>;
export declare function toolVariationFromJSON(jsonString: string): SafeParseResult<ToolVariation, SDKValidationError>;
//# sourceMappingURL=toolvariation.d.ts.map