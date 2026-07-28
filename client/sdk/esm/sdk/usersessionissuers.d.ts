import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { MigrateLegacyGramRegistrationsResult } from "../models/components/migratelegacygramregistrationsresult.js";
import { UserSessionIssuer } from "../models/components/usersessionissuer.js";
import {
  CreateUserSessionIssuerRequest,
  CreateUserSessionIssuerSecurity,
} from "../models/operations/createusersessionissuer.js";
import {
  DeleteUserSessionIssuerRequest,
  DeleteUserSessionIssuerSecurity,
} from "../models/operations/deleteusersessionissuer.js";
import {
  GetUserSessionIssuerRequest,
  GetUserSessionIssuerSecurity,
} from "../models/operations/getusersessionissuer.js";
import {
  ListUserSessionIssuersRequest,
  ListUserSessionIssuersResponse,
  ListUserSessionIssuersSecurity,
} from "../models/operations/listusersessionissuers.js";
import {
  MigrateLegacyGramRegistrationsRequest,
  MigrateLegacyGramRegistrationsSecurity,
} from "../models/operations/migratelegacygramregistrations.js";
import {
  UpdateUserSessionIssuerRequest,
  UpdateUserSessionIssuerSecurity,
} from "../models/operations/updateusersessionissuer.js";
import { PageIterator } from "../types/operations.js";
export declare class UserSessionIssuers extends ClientSDK {
  /**
   * createUserSessionIssuer userSessionIssuers
   *
   * @remarks
   * Create a new user_session_issuer.
   */
  create(
    request: CreateUserSessionIssuerRequest,
    security?: CreateUserSessionIssuerSecurity | undefined,
    options?: RequestOptions,
  ): Promise<UserSessionIssuer>;
  /**
   * deleteUserSessionIssuer userSessionIssuers
   *
   * @remarks
   * Soft-delete a user_session_issuer. Cascades to dependent user_sessions, user_session_consents, and remote_session_clients.
   */
  delete(
    request: DeleteUserSessionIssuerRequest,
    security?: DeleteUserSessionIssuerSecurity | undefined,
    options?: RequestOptions,
  ): Promise<void>;
  /**
   * getUserSessionIssuer userSessionIssuers
   *
   * @remarks
   * Get a user_session_issuer by id or by slug. Provide exactly one.
   */
  get(
    request?: GetUserSessionIssuerRequest | undefined,
    security?: GetUserSessionIssuerSecurity | undefined,
    options?: RequestOptions,
  ): Promise<UserSessionIssuer>;
  /**
   * listUserSessionIssuers userSessionIssuers
   *
   * @remarks
   * List user_session_issuers in the caller's project.
   */
  list(
    request?: ListUserSessionIssuersRequest | undefined,
    security?: ListUserSessionIssuersSecurity | undefined,
    options?: RequestOptions,
  ): Promise<
    PageIterator<
      ListUserSessionIssuersResponse,
      {
        cursor: string;
      }
    >
  >;
  /**
   * migrateLegacyGramRegistrations userSessionIssuers
   *
   * @remarks
   * One-off migration: lift the legacy Redis dynamic-client registrations from a gram-type oauth_proxy_provider into user_session_clients on the given user_session_issuer, so migrated MCP clients skip re-registration and re-auth. Removed once the OAuth proxy is retired.
   */
  migrateLegacyGramRegistrations(
    request: MigrateLegacyGramRegistrationsRequest,
    security?: MigrateLegacyGramRegistrationsSecurity | undefined,
    options?: RequestOptions,
  ): Promise<MigrateLegacyGramRegistrationsResult>;
  /**
   * updateUserSessionIssuer userSessionIssuers
   *
   * @remarks
   * Update fields on an existing user_session_issuer.
   */
  update(
    request: UpdateUserSessionIssuerRequest,
    security?: UpdateUserSessionIssuerSecurity | undefined,
    options?: RequestOptions,
  ): Promise<UserSessionIssuer>;
}
//# sourceMappingURL=usersessionissuers.d.ts.map
