import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type PromptTemplateEntry = {
    /**
     * The ID of the prompt template
     */
    id: string;
    /**
     * The kind of the prompt template
     */
    kind?: string | undefined;
    /**
     * A short url-friendly label that uniquely identifies a resource.
     */
    name: string;
};
/** @internal */
export declare const PromptTemplateEntry$inboundSchema: z.ZodMiniType<PromptTemplateEntry, unknown>;
export declare function promptTemplateEntryFromJSON(jsonString: string): SafeParseResult<PromptTemplateEntry, SDKValidationError>;
//# sourceMappingURL=prompttemplateentry.d.ts.map