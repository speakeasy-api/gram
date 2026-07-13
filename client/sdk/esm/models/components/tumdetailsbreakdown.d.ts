import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { TumDetailsBreakdownRow } from "./tumdetailsbreakdownrow.js";
/**
 * Per-dimension token breakdown for the usage details table
 */
export type TumDetailsBreakdown = {
    /**
     * The breakdown dimension key (matches telemetry.query group_by)
     */
    key: string;
    /**
     * Top values by tokens in descending order, with the remainder rolled into 'Other'
     */
    rows: Array<TumDetailsBreakdownRow>;
};
/** @internal */
export declare const TumDetailsBreakdown$inboundSchema: z.ZodMiniType<TumDetailsBreakdown, unknown>;
export declare function tumDetailsBreakdownFromJSON(jsonString: string): SafeParseResult<TumDetailsBreakdown, SDKValidationError>;
//# sourceMappingURL=tumdetailsbreakdown.d.ts.map