import * as z from "zod/v3";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { PaginationResponse } from "./paginationresponse.js";
import { ToolExecutionLog } from "./toolexecutionlog.js";
/**
 * Result of listing tool execution logs
 */
export type ListToolExecutionLogsResult = {
  /**
   * List of tool execution logs
   */
  logs?: Array<ToolExecutionLog> | undefined;
  /**
   * Pagination metadata for list responses
   */
  pagination?: PaginationResponse | undefined;
};
/** @internal */
export declare const ListToolExecutionLogsResult$inboundSchema: z.ZodType<
  ListToolExecutionLogsResult,
  z.ZodTypeDef,
  unknown
>;
export declare function listToolExecutionLogsResultFromJSON(
  jsonString: string,
): SafeParseResult<ListToolExecutionLogsResult, SDKValidationError>;
//# sourceMappingURL=listtoolexecutionlogsresult.d.ts.map
