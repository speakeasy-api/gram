import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type RenderTemplateResult = {
    /**
     * The rendered prompt
     */
    prompt: string;
};
/** @internal */
export declare const RenderTemplateResult$inboundSchema: z.ZodMiniType<RenderTemplateResult, unknown>;
export declare function renderTemplateResultFromJSON(jsonString: string): SafeParseResult<RenderTemplateResult, SDKValidationError>;
//# sourceMappingURL=rendertemplateresult.d.ts.map