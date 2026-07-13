import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { TraceSummaryRecord } from "./tracesummaryrecord.js";
/**
 * Result of listing trace summaries
 */
export type ListTracesResult = {
  /**
   * Cursor for next page (trace ID)
   */
  nextCursor?: string | undefined;
  /**
   * List of trace summaries
   */
  traces: Array<TraceSummaryRecord>;
};
/** @internal */
export declare const ListTracesResult$inboundSchema: z.ZodMiniType<
  ListTracesResult,
  unknown
>;
export declare function listTracesResultFromJSON(
  jsonString: string,
): SafeParseResult<ListTracesResult, SDKValidationError>;
//# sourceMappingURL=listtracesresult.d.ts.map
