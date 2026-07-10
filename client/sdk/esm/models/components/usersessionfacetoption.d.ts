import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type UserSessionFacetOption = {
  /**
   * Number of sessions for this facet value.
   */
  count: number;
  /**
   * The label shown for the facet value.
   */
  displayName: string;
  /**
   * The facet value used for filtering.
   */
  value: string;
};
/** @internal */
export declare const UserSessionFacetOption$inboundSchema: z.ZodMiniType<
  UserSessionFacetOption,
  unknown
>;
export declare function userSessionFacetOptionFromJSON(
  jsonString: string,
): SafeParseResult<UserSessionFacetOption, SDKValidationError>;
//# sourceMappingURL=usersessionfacetoption.d.ts.map
