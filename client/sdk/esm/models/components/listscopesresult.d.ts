import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ScopeDefinition } from "./scopedefinition.js";
export type ListScopesResult = {
  /**
   * The scopes available in access control.
   */
  scopes: Array<ScopeDefinition>;
};
/** @internal */
export declare const ListScopesResult$inboundSchema: z.ZodMiniType<
  ListScopesResult,
  unknown
>;
export declare function listScopesResultFromJSON(
  jsonString: string,
): SafeParseResult<ListScopesResult, SDKValidationError>;
//# sourceMappingURL=listscopesresult.d.ts.map
