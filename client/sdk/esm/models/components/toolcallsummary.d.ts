import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Summary information for a tool call
 */
export type ToolCallSummary = {
  /**
   * Event source (from attributes.gram.event.source)
   */
  eventSource?: string | undefined;
  /**
   * Gram URN associated with this tool call
   */
  gramUrn: string;
  /**
   * HTTP status code (if applicable)
   */
  httpStatusCode?: number | undefined;
  /**
   * Total number of logs in this tool call
   */
  logCount: number;
  /**
   * Earliest log timestamp in Unix nanoseconds (string for JS int64 precision)
   */
  startTimeUnixNano: string;
  /**
   * Tool name (from attributes.gram.tool.name)
   */
  toolName?: string | undefined;
  /**
   * Tool call source (from attributes.gram.tool_call.source)
   */
  toolSource?: string | undefined;
  /**
   * Trace ID (32 hex characters)
   */
  traceId: string;
};
/** @internal */
export declare const ToolCallSummary$inboundSchema: z.ZodMiniType<
  ToolCallSummary,
  unknown
>;
export declare function toolCallSummaryFromJSON(
  jsonString: string,
): SafeParseResult<ToolCallSummary, SDKValidationError>;
//# sourceMappingURL=toolcallsummary.d.ts.map
