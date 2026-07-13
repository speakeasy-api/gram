import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { RoleSummary } from "./rolesummary.js";
import { UserSummary } from "./usersummary.js";
/**
 * Result of searching user usage summaries
 */
export type SearchUsersResult = {
  /**
   * Cursor for next page
   */
  nextCursor?: string | undefined;
  /**
   * List of role usage summaries (populated when group_by=role)
   */
  roles?: Array<RoleSummary> | undefined;
  /**
   * List of user usage summaries (populated when group_by=employee)
   */
  users: Array<UserSummary>;
};
/** @internal */
export declare const SearchUsersResult$inboundSchema: z.ZodMiniType<
  SearchUsersResult,
  unknown
>;
export declare function searchUsersResultFromJSON(
  jsonString: string,
): SafeParseResult<SearchUsersResult, SDKValidationError>;
//# sourceMappingURL=searchusersresult.d.ts.map
