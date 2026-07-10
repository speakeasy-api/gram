import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { QueryPoint } from "./querypoint.js";
/**
 * A gap-filled timeseries for a single group value (one line on the chart).
 */
export type QuerySeries = {
    /**
     * The dimension value for this series. Empty string when no group_by was requested; 'Other' for the rolled-up remainder beyond top_n.
     */
    groupValue: string;
    /**
     * Time buckets in ascending order, gap-filled with zeros.
     */
    points: Array<QueryPoint>;
};
/** @internal */
export declare const QuerySeries$inboundSchema: z.ZodMiniType<QuerySeries, unknown>;
export declare function querySeriesFromJSON(jsonString: string): SafeParseResult<QuerySeries, SDKValidationError>;
//# sourceMappingURL=queryseries.d.ts.map