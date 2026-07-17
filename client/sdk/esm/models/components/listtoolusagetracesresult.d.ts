import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ToolUsageTraceSummary } from "./toolusagetracesummary.js";
/**
 * Result of listing target-aware MCP and tool usage traces
 */
export type ListToolUsageTracesResult = {
  /**
   * Cursor for next page
   */
  nextCursor?: string | undefined;
  /**
   * Target-aware tool usage trace rows
   */
  traces: Array<ToolUsageTraceSummary>;
};
/** @internal */
export declare const ListToolUsageTracesResult$inboundSchema: z.ZodMiniType<
  ListToolUsageTracesResult,
  unknown
>;
export declare function listToolUsageTracesResultFromJSON(
  jsonString: string,
): SafeParseResult<ListToolUsageTracesResult, SDKValidationError>;
//# sourceMappingURL=listtoolusagetracesresult.d.ts.map
