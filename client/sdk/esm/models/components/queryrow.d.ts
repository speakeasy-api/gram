import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { QueryMeasures } from "./querymeasures.js";
/**
 * One row of the grouped table: measures aggregated over the full time range for a single group value.
 */
export type QueryRow = {
  /**
   * Distinct values of every allowlisted dimension other than the group_by dimension, observed within this group. Keyed by dimension identifier (the same keys used for group_by/filters, e.g. when grouping by department_name: 'email' -> [...], 'job_title' -> [...], 'role' -> [...]). Empty values are omitted and each list is capped.
   */
  dimensionValues: {
    [k: string]: Array<string>;
  };
  /**
   * The dimension value for this row. Empty string when no group_by was requested; 'Other' for the rolled-up remainder beyond top_n.
   */
  groupValue: string;
  /**
   * Aggregated measure values for a group or time bucket
   */
  measures: QueryMeasures;
};
/** @internal */
export declare const QueryRow$inboundSchema: z.ZodMiniType<QueryRow, unknown>;
export declare function queryRowFromJSON(
  jsonString: string,
): SafeParseResult<QueryRow, SDKValidationError>;
//# sourceMappingURL=queryrow.d.ts.map
