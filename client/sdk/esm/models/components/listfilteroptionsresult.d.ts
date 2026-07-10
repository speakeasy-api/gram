import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { FilterOption } from "./filteroption.js";
/**
 * Result of listing filter options
 */
export type ListFilterOptionsResult = {
    /**
     * List of filter options
     */
    options: Array<FilterOption>;
};
/** @internal */
export declare const ListFilterOptionsResult$inboundSchema: z.ZodMiniType<ListFilterOptionsResult, unknown>;
export declare function listFilterOptionsResultFromJSON(jsonString: string): SafeParseResult<ListFilterOptionsResult, SDKValidationError>;
//# sourceMappingURL=listfilteroptionsresult.d.ts.map