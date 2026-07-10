import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * A single filter option (API key or user)
 */
export type FilterOption = {
    /**
     * Number of events for this option
     */
    count: number;
    /**
     * Unique identifier for the option
     */
    id: string;
    /**
     * Display label for the option
     */
    label: string;
};
/** @internal */
export declare const FilterOption$inboundSchema: z.ZodMiniType<FilterOption, unknown>;
export declare function filterOptionFromJSON(jsonString: string): SafeParseResult<FilterOption, SDKValidationError>;
//# sourceMappingURL=filteroption.d.ts.map