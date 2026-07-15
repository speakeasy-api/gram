import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * One value of a breakdown dimension with its token usage over the range
 */
export type TumDetailsBreakdownRow = {
    /**
     * Daily tokens aligned to the result's points buckets
     */
    series: Array<number>;
    /**
     * Tokens for this value over the range
     */
    totalTokens: number;
    /**
     * The dimension value; empty for rows without the attribute, 'Other' for the top-N remainder rollup
     */
    value: string;
};
/** @internal */
export declare const TumDetailsBreakdownRow$inboundSchema: z.ZodMiniType<TumDetailsBreakdownRow, unknown>;
export declare function tumDetailsBreakdownRowFromJSON(jsonString: string): SafeParseResult<TumDetailsBreakdownRow, SDKValidationError>;
//# sourceMappingURL=tumdetailsbreakdownrow.d.ts.map