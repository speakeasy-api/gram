import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type ListSourcesResult = {
  /**
   * The distinct agent sources present in this project's chats (raw source strings such as 'claude-code', 'Codex', 'playground').
   */
  sources: Array<string>;
};
/** @internal */
export declare const ListSourcesResult$inboundSchema: z.ZodMiniType<
  ListSourcesResult,
  unknown
>;
export declare function listSourcesResultFromJSON(
  jsonString: string,
): SafeParseResult<ListSourcesResult, SDKValidationError>;
//# sourceMappingURL=listsourcesresult.d.ts.map
