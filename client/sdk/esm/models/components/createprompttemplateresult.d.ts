import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { PromptTemplate } from "./prompttemplate.js";
export type CreatePromptTemplateResult = {
    /**
     * A prompt template
     */
    template: PromptTemplate;
};
/** @internal */
export declare const CreatePromptTemplateResult$inboundSchema: z.ZodMiniType<CreatePromptTemplateResult, unknown>;
export declare function createPromptTemplateResultFromJSON(jsonString: string): SafeParseResult<CreatePromptTemplateResult, SDKValidationError>;
//# sourceMappingURL=createprompttemplateresult.d.ts.map