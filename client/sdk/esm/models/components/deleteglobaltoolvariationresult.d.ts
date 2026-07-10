import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type DeleteGlobalToolVariationResult = {
    /**
     * The ID of the variation that was deleted
     */
    variationId: string;
};
/** @internal */
export declare const DeleteGlobalToolVariationResult$inboundSchema: z.ZodMiniType<DeleteGlobalToolVariationResult, unknown>;
export declare function deleteGlobalToolVariationResultFromJSON(jsonString: string): SafeParseResult<DeleteGlobalToolVariationResult, SDKValidationError>;
//# sourceMappingURL=deleteglobaltoolvariationresult.d.ts.map