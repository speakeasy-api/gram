import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { UserSessionIssuer } from "./usersessionissuer.js";
/**
 * Result type for listing user_session_issuers.
 */
export type ListUserSessionIssuersResult = {
  items: Array<UserSessionIssuer>;
  /**
   * Cursor for the next page; empty when exhausted.
   */
  nextCursor?: string | undefined;
};
/** @internal */
export declare const ListUserSessionIssuersResult$inboundSchema: z.ZodMiniType<
  ListUserSessionIssuersResult,
  unknown
>;
export declare function listUserSessionIssuersResultFromJSON(
  jsonString: string,
): SafeParseResult<ListUserSessionIssuersResult, SDKValidationError>;
//# sourceMappingURL=listusersessionissuersresult.d.ts.map
