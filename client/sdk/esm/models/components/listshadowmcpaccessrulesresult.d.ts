import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ShadowMCPAccessRule } from "./shadowmcpaccessrule.js";
export type ListShadowMCPAccessRulesResult = {
  /**
   * Cursor for the next page of results.
   */
  nextCursor?: string | undefined;
  rules: Array<ShadowMCPAccessRule>;
};
/** @internal */
export declare const ListShadowMCPAccessRulesResult$inboundSchema: z.ZodMiniType<
  ListShadowMCPAccessRulesResult,
  unknown
>;
export declare function listShadowMCPAccessRulesResultFromJSON(
  jsonString: string,
): SafeParseResult<ListShadowMCPAccessRulesResult, SDKValidationError>;
//# sourceMappingURL=listshadowmcpaccessrulesresult.d.ts.map
