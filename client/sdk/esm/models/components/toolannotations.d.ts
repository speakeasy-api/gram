import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Tool annotations providing behavioral hints about the tool
 */
export type ToolAnnotations = {
    /**
     * If true, the tool may perform destructive updates (only meaningful when read_only_hint is false)
     */
    destructiveHint?: boolean | undefined;
    /**
     * If true, repeated calls with same arguments have no additional effect (only meaningful when read_only_hint is false)
     */
    idempotentHint?: boolean | undefined;
    /**
     * If true, the tool interacts with external entities beyond its local environment
     */
    openWorldHint?: boolean | undefined;
    /**
     * If true, the tool does not modify its environment
     */
    readOnlyHint?: boolean | undefined;
    /**
     * Human-readable display name for the tool
     */
    title?: string | undefined;
};
/** @internal */
export declare const ToolAnnotations$inboundSchema: z.ZodMiniType<ToolAnnotations, unknown>;
export declare function toolAnnotationsFromJSON(jsonString: string): SafeParseResult<ToolAnnotations, SDKValidationError>;
//# sourceMappingURL=toolannotations.d.ts.map