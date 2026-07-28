import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { HookTraceSummary } from "./hooktracesummary.js";
/**
 * Result of listing hook traces
 */
export type ListHooksTracesResult = {
  /**
   * Cursor for next page
   */
  nextCursor?: string | undefined;
  /**
   * List of hook trace summaries
   */
  traces: Array<HookTraceSummary>;
};
/** @internal */
export declare const ListHooksTracesResult$inboundSchema: z.ZodMiniType<
  ListHooksTracesResult,
  unknown
>;
export declare function listHooksTracesResultFromJSON(
  jsonString: string,
): SafeParseResult<ListHooksTracesResult, SDKValidationError>;
//# sourceMappingURL=listhookstracesresult.d.ts.map
