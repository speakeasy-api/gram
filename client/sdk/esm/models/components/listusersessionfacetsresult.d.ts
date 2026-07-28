import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { UserSessionFacetOption } from "./usersessionfacetoption.js";
export type ListUserSessionFacetsResult = {
  /**
   * Connecting client facets.
   */
  clients: Array<UserSessionFacetOption>;
  /**
   * Issuer/server facets.
   */
  servers: Array<UserSessionFacetOption>;
  /**
   * Subject (user) facets.
   */
  users: Array<UserSessionFacetOption>;
};
/** @internal */
export declare const ListUserSessionFacetsResult$inboundSchema: z.ZodMiniType<
  ListUserSessionFacetsResult,
  unknown
>;
export declare function listUserSessionFacetsResultFromJSON(
  jsonString: string,
): SafeParseResult<ListUserSessionFacetsResult, SDKValidationError>;
//# sourceMappingURL=listusersessionfacetsresult.d.ts.map
