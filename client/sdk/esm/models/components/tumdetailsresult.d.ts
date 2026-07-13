import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { TumDetailsBreakdown } from "./tumdetailsbreakdown.js";
import { TumDetailsPoint } from "./tumdetailspoint.js";
import { TumDetailsTotals } from "./tumdetailstotals.js";
/**
 * Result of the billing usage details query
 */
export type TumDetailsResult = {
    /**
     * Token usage per breakdown dimension, one entry per supported dimension
     */
    breakdowns: Array<TumDetailsBreakdown>;
    /**
     * Timeseries bucket width in seconds. Always 86400 — the details are bucketed daily.
     */
    intervalSeconds: number;
    /**
     * Gap-filled daily buckets in ascending time order
     */
    points: Array<TumDetailsPoint>;
    /**
     * Whole-range totals for the billing usage details. Distinct counts (sessions, active users) are computed over the full range and cannot be derived by summing the daily points.
     */
    totals: TumDetailsTotals;
};
/** @internal */
export declare const TumDetailsResult$inboundSchema: z.ZodMiniType<TumDetailsResult, unknown>;
export declare function tumDetailsResultFromJSON(jsonString: string): SafeParseResult<TumDetailsResult, SDKValidationError>;
//# sourceMappingURL=tumdetailsresult.d.ts.map