import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { PromptTemplate } from "./prompttemplate.js";
export type ListPromptTemplatesResult = {
    /**
     * The created prompt template
     */
    templates: Array<PromptTemplate>;
};
/** @internal */
export declare const ListPromptTemplatesResult$inboundSchema: z.ZodMiniType<ListPromptTemplatesResult, unknown>;
export declare function listPromptTemplatesResultFromJSON(jsonString: string): SafeParseResult<ListPromptTemplatesResult, SDKValidationError>;
//# sourceMappingURL=listprompttemplatesresult.d.ts.map