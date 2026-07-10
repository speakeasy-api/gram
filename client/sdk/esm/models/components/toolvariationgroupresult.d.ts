import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ToolVariationGroup } from "./toolvariationgroup.js";
export type ToolVariationGroupResult = {
    group: ToolVariationGroup;
};
/** @internal */
export declare const ToolVariationGroupResult$inboundSchema: z.ZodMiniType<ToolVariationGroupResult, unknown>;
export declare function toolVariationGroupResultFromJSON(jsonString: string): SafeParseResult<ToolVariationGroupResult, SDKValidationError>;
//# sourceMappingURL=toolvariationgroupresult.d.ts.map