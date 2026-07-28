import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { SessionSummary } from "./sessionsummary.js";
/**
 * Result of listing org-scoped chat sessions
 */
export type ListSessionsResult = {
  /**
   * Cursor for next page
   */
  nextCursor?: string | undefined;
  /**
   * List of chat session summaries
   */
  sessions: Array<SessionSummary>;
};
/** @internal */
export declare const ListSessionsResult$inboundSchema: z.ZodMiniType<
  ListSessionsResult,
  unknown
>;
export declare function listSessionsResultFromJSON(
  jsonString: string,
): SafeParseResult<ListSessionsResult, SDKValidationError>;
//# sourceMappingURL=listsessionsresult.d.ts.map
