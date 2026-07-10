import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { QueryRow } from "./queryrow.js";
import { QuerySeries } from "./queryseries.js";
/**
 * Result of a generic analytics query: a grouped table and a matching per-group timeseries over the same data slice.
 */
export type QueryResult = {
    /**
     * Echoes the requested group_by dimension; empty when none was requested.
     */
    groupBy: string;
    /**
     * The timeseries bucket interval in seconds.
     */
    intervalSeconds: number;
    /**
     * Grouped totals over the full time range, ordered by sort_by descending.
     */
    table: Array<QueryRow>;
    /**
     * One series per group value (aligned with table rows), each gap-filled.
     */
    timeseries: Array<QuerySeries>;
};
/** @internal */
export declare const QueryResult$inboundSchema: z.ZodMiniType<QueryResult, unknown>;
export declare function queryResultFromJSON(jsonString: string): SafeParseResult<QueryResult, SDKValidationError>;
//# sourceMappingURL=queryresult.d.ts.map