import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { PromptTemplate } from "./prompttemplate.js";
export type UpdatePromptTemplateResult = {
    /**
     * A prompt template
     */
    template: PromptTemplate;
};
/** @internal */
export declare const UpdatePromptTemplateResult$inboundSchema: z.ZodMiniType<UpdatePromptTemplateResult, unknown>;
export declare function updatePromptTemplateResultFromJSON(jsonString: string): SafeParseResult<UpdatePromptTemplateResult, SDKValidationError>;
//# sourceMappingURL=updateprompttemplateresult.d.ts.map