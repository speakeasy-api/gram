import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ToolVariation } from "./toolvariation.js";
export type UpsertGlobalToolVariationResult = {
    variation: ToolVariation;
};
/** @internal */
export declare const UpsertGlobalToolVariationResult$inboundSchema: z.ZodMiniType<UpsertGlobalToolVariationResult, unknown>;
export declare function upsertGlobalToolVariationResultFromJSON(jsonString: string): SafeParseResult<UpsertGlobalToolVariationResult, SDKValidationError>;
//# sourceMappingURL=upsertglobaltoolvariationresult.d.ts.map