import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { UserSessionClient } from "./usersessionclient.js";
/**
 * Result type for listing user_session_clients.
 */
export type ListUserSessionClientsResult = {
  items: Array<UserSessionClient>;
  /**
   * Cursor for the next page; empty when exhausted.
   */
  nextCursor?: string | undefined;
};
/** @internal */
export declare const ListUserSessionClientsResult$inboundSchema: z.ZodMiniType<
  ListUserSessionClientsResult,
  unknown
>;
export declare function listUserSessionClientsResultFromJSON(
  jsonString: string,
): SafeParseResult<ListUserSessionClientsResult, SDKValidationError>;
//# sourceMappingURL=listusersessionclientsresult.d.ts.map
