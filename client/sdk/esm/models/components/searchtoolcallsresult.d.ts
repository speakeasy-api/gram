import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ToolCallSummary } from "./toolcallsummary.js";
/**
 * Result of searching tool call summaries
 */
export type SearchToolCallsResult = {
  /**
   * Cursor for next page
   */
  nextCursor?: string | undefined;
  /**
   * List of tool call summaries
   */
  toolCalls: Array<ToolCallSummary>;
};
/** @internal */
export declare const SearchToolCallsResult$inboundSchema: z.ZodMiniType<
  SearchToolCallsResult,
  unknown
>;
export declare function searchToolCallsResultFromJSON(
  jsonString: string,
): SafeParseResult<SearchToolCallsResult, SDKValidationError>;
//# sourceMappingURL=searchtoolcallsresult.d.ts.map
