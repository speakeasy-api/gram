import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Result for Codex hook events
 */
export type CodexHookResult = {
  /**
   * Permission decision for blocking events: allow or deny
   */
  decision?: string | undefined;
  /**
   * Reason for the decision, shown to the user
   */
  reason?: string | undefined;
};
/** @internal */
export declare const CodexHookResult$inboundSchema: z.ZodMiniType<
  CodexHookResult,
  unknown
>;
export declare function codexHookResultFromJSON(
  jsonString: string,
): SafeParseResult<CodexHookResult, SDKValidationError>;
//# sourceMappingURL=codexhookresult.d.ts.map
