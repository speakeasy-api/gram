import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type AssistantToolsetRef = {
    /**
     * Optional environment slug used when invoking the toolset.
     */
    environmentSlug?: string | undefined;
    /**
     * The toolset slug exposed to the assistant.
     */
    toolsetSlug: string;
};
/** @internal */
export declare const AssistantToolsetRef$inboundSchema: z.ZodMiniType<AssistantToolsetRef, unknown>;
/** @internal */
export type AssistantToolsetRef$Outbound = {
    environment_slug?: string | undefined;
    toolset_slug: string;
};
/** @internal */
export declare const AssistantToolsetRef$outboundSchema: z.ZodMiniType<AssistantToolsetRef$Outbound, AssistantToolsetRef>;
export declare function assistantToolsetRefToJSON(assistantToolsetRef: AssistantToolsetRef): string;
export declare function assistantToolsetRefFromJSON(jsonString: string): SafeParseResult<AssistantToolsetRef, SDKValidationError>;
//# sourceMappingURL=assistanttoolsetref.d.ts.map