import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * A user_session_client (DCR'd MCP client). client_secret_hash is never returned.
 */
export type UserSessionClient = {
  /**
   * DCR-issued client_id.
   */
  clientId: string;
  clientIdIssuedAt: Date;
  /**
   * Display name from the registration request.
   */
  clientName: string;
  /**
   * Null when the secret does not expire.
   */
  clientSecretExpiresAt?: Date | undefined;
  createdAt: Date;
  /**
   * The user_session_client id.
   */
  id: string;
  /**
   * Validated on every /authorize.
   */
  redirectUris: Array<string>;
  updatedAt: Date;
  /**
   * The owning user_session_issuer id.
   */
  userSessionIssuerId: string;
};
/** @internal */
export declare const UserSessionClient$inboundSchema: z.ZodMiniType<
  UserSessionClient,
  unknown
>;
export declare function userSessionClientFromJSON(
  jsonString: string,
): SafeParseResult<UserSessionClient, SDKValidationError>;
//# sourceMappingURL=usersessionclient.d.ts.map
