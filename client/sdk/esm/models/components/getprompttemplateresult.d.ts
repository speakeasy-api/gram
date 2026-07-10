import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { PromptTemplate } from "./prompttemplate.js";
export type GetPromptTemplateResult = {
    /**
     * A prompt template
     */
    template: PromptTemplate;
};
/** @internal */
export declare const GetPromptTemplateResult$inboundSchema: z.ZodMiniType<GetPromptTemplateResult, unknown>;
export declare function getPromptTemplateResultFromJSON(jsonString: string): SafeParseResult<GetPromptTemplateResult, SDKValidationError>;
//# sourceMappingURL=getprompttemplateresult.d.ts.map